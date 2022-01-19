// Package iface implements basics of the face system.
package iface

/*
#include "../csrc/iface/face.h"
*/
import "C"
import (
	"fmt"
	"io"
	"unsafe"

	"github.com/pkg/math"
	"github.com/usnistgov/ndn-dpdk/core/cptr"
	"github.com/usnistgov/ndn-dpdk/core/logging"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ringbuffer"
	"github.com/usnistgov/ndn-dpdk/ndni"
	"go.uber.org/zap"
	"go4.org/must"
)

var logger = logging.New("iface")

// Face represents a network layer face.
type Face interface {
	eal.WithNumaSocket
	io.Closer

	// Ptr returns *C.Face pointer.
	Ptr() unsafe.Pointer

	// ID returns face ID.
	ID() ID

	// Locator returns a Locator describing face endpoints.
	Locator() Locator

	// Counters returns basic face counters.
	Counters() Counters

	// ReadExCounters returns extended counters.
	ReadExCounters() interface{}

	// TxAlign returns TX packet alignment requirement.
	TxAlign() ndni.PacketTxAlign

	// SetDown changes face UP/DOWN state.
	SetDown(isDown bool)
}

// Config contains face configuration.
type Config struct {
	// ReassemblerCapacity is the partial message store capacity in the reassembler.
	//
	// If this value is zero, it defaults to DefaultReassemblerCapacity.
	// Otherwise, it is clamped between MinReassemblerCapacity and MaxReassemblerCapacity.
	ReassemblerCapacity int `json:"reassemblerCapacity,omitempty"`

	// OutputQueueSize is the packet queue capacity before the output thread.
	//
	// The minimum is MinOutputQueueSize.
	// If this value is less than the minimum, it defaults to DefaultOutputQueueSize.
	// Otherwise, it is adjusted up to the next power of 2.
	OutputQueueSize int `json:"outputQueueSize,omitempty"`

	// MTU is the maximum size of outgoing NDNLP packets.
	// This excludes lower layer headers, such as Ethernet/VXLAN headers.
	//
	// Default is the lesser of MaxMTU and what's allowed by network interface and lower layer protocols.
	// If this is less than MinMTU or greater than the maximum, the face will fail to initialize.
	MTU int `json:"mtu,omitempty"`

	maxMTU int
}

// ApplyDefaults applies defaults.
func (c *Config) ApplyDefaults() {
	if c.ReassemblerCapacity == 0 {
		c.ReassemblerCapacity = DefaultReassemblerCapacity
	}
	c.ReassemblerCapacity = math.MinInt(math.MaxInt(MinReassemblerCapacity, c.ReassemblerCapacity), MaxReassemblerCapacity)

	c.OutputQueueSize = ringbuffer.AlignCapacity(c.OutputQueueSize, MinOutputQueueSize, DefaultOutputQueueSize)
}

// WithMaxMTU returns a copy of Config with consideration of device MTU.
func (c Config) WithMaxMTU(max int) Config {
	c.maxMTU = math.MinInt(max, MaxMTU)
	return c
}

func (c *Config) checkMTU() error {
	if c.maxMTU == 0 {
		c.maxMTU = MaxMTU
	}
	if c.MTU == 0 {
		c.MTU = c.maxMTU
	}
	if c.MTU < MinMTU || c.MTU > c.maxMTU {
		return fmt.Errorf("face MTU must be between %d and %d", MinMTU, c.maxMTU)
	}
	return nil
}

// NewParams contains parameters to New().
type NewParams struct {
	Config

	// Socket indicates where to allocate memory.
	Socket eal.NumaSocket

	// SizeOfPriv is the size of C.FaceImpl.priv struct.
	SizeofPriv uintptr

	// Init callback is invoked after allocating C.FaceImpl.
	// This is always invoked on the main thread.
	Init func(f Face) (InitResult, error)

	// Start callback is invoked after data structure initialization.
	// It should activate the face in RxLoop and TxLoop.
	// It returns a 'subclass' Face interface implementation to make available via Get(id).
	// This is always invoked on the main thread.
	Start func() error

	// Locator callback returns a Locator describing the face.
	Locator func() Locator

	// Stop callback is invoked to stop the face.
	// It should deactivate the face in RxLoop and TxLoop.
	// This is always invoked on the main thread.
	Stop func() error

	// Close callback is invoked after the face has been removed.
	// This is optional.
	// This is always invoked on the main thread.
	Close func() error

	// ReadExCounters callback returns extended counters.
	// This is optional.
	ReadExCounters func() interface{}
}

// InitResult contains results of NewParams.Init callback.
type InitResult struct {
	// Face is a Face interface implementation that would be returned via Get(id).
	// It must embed the base Face passed to NewParams.Init().
	Face Face

	// TxLinearize indicates whether TX mbufs must be direct mbufs in contiguous memory.
	// See C.PacketTxAlign.linearize field.
	TxLinearize bool

	// L2TxBurst is a C function of C.Face_L2TxBurst type.
	L2TxBurst unsafe.Pointer
}

// New creates a Face.
func New(p NewParams) (face Face, e error) {
	p.Config.ApplyDefaults()
	if e = p.Config.checkMTU(); e != nil {
		return nil, e
	}
	if p.Socket.IsAny() {
		p.Socket = eal.RandomSocket()
	}

	eal.CallMain(func() {
		face, e = newFace(p)
	})
	return
}

func newFace(p NewParams) (Face, error) {
	f := &face{
		id:                     AllocID(),
		socket:                 p.Socket,
		locatorCallback:        p.Locator,
		stopCallback:           p.Stop,
		closeCallback:          p.Close,
		readExCountersCallback: p.ReadExCounters,
	}
	logEntry := logger.With(
		f.id.ZapField("id"),
		p.Socket.ZapField("socket"),
		zap.Int("mtu", p.MTU),
	)

	c := f.ptr()
	c.id = C.FaceID(f.id)
	c.state = StateUp
	c.impl = (*C.FaceImpl)(eal.ZmallocAligned("FaceImpl", C.sizeof_FaceImpl+p.SizeofPriv, 1, p.Socket))

	initResult, e := p.Init(f)
	if e != nil {
		logEntry.Warn("init error", zap.Error(e))
		return f.clear(), e
	}
	if initResult.Face.ID() != f.id {
		panic("initResult.Face should embed base Face")
	}
	logEntry = logEntry.With(LocatorZapField("locator", f.Locator()))

	c.txAlign = C.PacketTxAlign{
		linearize:           C.bool(initResult.TxLinearize),
		fragmentPayloadSize: C.uint16_t(p.MTU - ndni.LpHeaderHeadroom),
	}
	c.impl.tx.l2Burst = (C.Face_L2TxBurst)(initResult.L2TxBurst)
	(*ndni.Mempools)(unsafe.Pointer(&c.impl.tx.mp)).Assign(p.Socket)

	outputQueue, e := ringbuffer.New(p.OutputQueueSize, p.Socket, ringbuffer.ProducerMulti, ringbuffer.ConsumerSingle)
	if e != nil {
		logEntry.Warn("outputQueue error", zap.Error(e))
		return f.clear(), e
	}
	c.outputQueue = (*C.struct_rte_ring)(outputQueue.Ptr())

	for i := 0; i < MaxRxProcThreads; i++ {
		ok := func() bool {
			reassID := C.CString(eal.AllocObjectID("iface.Reassembler"))
			defer C.free(unsafe.Pointer(reassID))
			return bool(C.Reassembler_Init(&c.impl.rx.threads[i].reass, reassID,
				C.uint32_t(p.ReassemblerCapacity), C.unsigned(p.Socket.ID())))
		}()
		if !ok {
			e := eal.GetErrno()
			logEntry.Warn("Reassembler_Init error", zap.Int("rx-thread", i), zap.Error(e))
			return f.clear(), e
		}
	}

	C.TxProc_Init(&c.impl.tx, c.txAlign)

	if e := p.Start(); e != nil {
		logEntry.Warn("start error", zap.Error(e))
		return f.clear(), e
	}

	gFaces[f.id] = initResult.Face
	emitter.Emit(evtFaceNew, f.id)
	logEntry.Info("face created")
	return initResult.Face, nil
}

type face struct {
	id                     ID
	socket                 eal.NumaSocket
	locatorCallback        func() Locator
	stopCallback           func() error
	closeCallback          func() error
	readExCountersCallback func() interface{}
}

func (f *face) ptr() *C.Face {
	return C.Face_Get(C.FaceID(f.id))
}

func (f *face) Ptr() unsafe.Pointer {
	return unsafe.Pointer(f.ptr())
}

func (f *face) ID() ID {
	return f.id
}

func (f *face) NumaSocket() eal.NumaSocket {
	return f.socket
}

func (f *face) Locator() Locator {
	return f.locatorCallback()
}

func (f *face) Close() (e error) {
	eal.CallMain(func() { e = f.close() })
	return e
}

func (f *face) close() error {
	f.ptr().state = StateDown
	emitter.Emit(evtFaceClosing, f.id)

	if e := f.stopCallback(); e != nil {
		return e
	}

	f.clear()
	emitter.Emit(evtFaceClosed, f.id)

	if f.closeCallback != nil {
		return f.closeCallback()
	}
	return nil
}

func (f *face) clear() Face {
	id, c := f.id, f.ptr()
	c.state = StateRemoved
	if c.impl != nil {
		for i := 0; i < MaxRxProcThreads; i++ {
			C.Reassembler_Close(&c.impl.rx.threads[i].reass)
		}
		eal.Free(c.impl)
	}
	if c.outputQueue != nil {
		must.Close(ringbuffer.FromPtr(unsafe.Pointer(c.outputQueue)))
	}
	c.id = 0
	gFaces[id] = nil
	return nil
}

func (f *face) ReadExCounters() interface{} {
	if f.readExCountersCallback != nil {
		return f.readExCountersCallback()
	}
	return nil
}

func (f *face) TxAlign() ndni.PacketTxAlign {
	return *(*ndni.PacketTxAlign)(unsafe.Pointer(&f.ptr().txAlign))
}

func (f *face) SetDown(isDown bool) {
	id, c := f.id, f.ptr()
	if IsDown(id) == isDown {
		return
	}
	if isDown {
		c.state = StateDown
		emitter.Emit(evtFaceDown, id)
	} else {
		c.state = StateUp
		emitter.Emit(evtFaceUp, id)
	}
}

// IsDown returns true if the face does not exist or is down.
func IsDown(id ID) bool {
	return bool(C.Face_IsDown(C.FaceID(id)))
}

// TxBurst transmits a burst of L3 packets.
func TxBurst(id ID, pkts []*ndni.Packet) {
	ptr, count := cptr.ParseCptrArray(pkts)
	C.Face_TxBurst(C.FaceID(id), (**C.Packet)(ptr), C.uint16_t(count))
}
