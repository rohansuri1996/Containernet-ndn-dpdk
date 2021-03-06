// Package socketface implements UDP/TCP socket faces using Go net.Conn type.
package socketface

/*
#include "../../csrc/iface/face.h"
extern uint16_t go_SocketFace_TxBurst(Face* faceC, struct rte_mbuf** pkts, uint16_t nPkts);
*/
import "C"
import (
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/core/nnduration"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/pktmbuf"
	"github.com/usnistgov/ndn-dpdk/iface"
	"github.com/usnistgov/ndn-dpdk/ndn/l3"
	"github.com/usnistgov/ndn-dpdk/ndn/sockettransport"
	"github.com/usnistgov/ndn-dpdk/ndni"
)

// Config contains socket face configuration.
type Config struct {
	iface.Config

	// RxGroupCapacity is the ring buffer capacity of the RX group shared among all socket faces.
	// Minimum is MinRxGroupCapacity. Default is DefaultRxGroupCapacity.
	// This can be changed only if no socket face is present, otherwise this is ignored.
	RxGroupCapacity int `json:"rxGroupCapacity,omitempty"`

	// sockettransport.Config fields.
	// See ndn-dpdk/ndn/sockettransport package for their semantics and defaults.
	RxQueueSize          int                     `json:"rxQueueSize,omitempty"`
	TxQueueSize          int                     `json:"txQueueSize,omitempty"`
	RedialBackoffInitial nnduration.Milliseconds `json:"redialBackoffInitial,omitempty"`
	RedialBackoffMaximum nnduration.Milliseconds `json:"redialBackoffMaximum,omitempty"`
}

// New creates a socket face.
func New(loc Locator) (iface.Face, error) {
	if e := loc.Validate(); e != nil {
		return nil, e
	}

	var cfg Config
	if loc.Config != nil {
		cfg = *loc.Config
	}

	var dialer sockettransport.Dialer
	dialer.RxBufferLength = ndni.PacketMempool.Config().Dataroom
	dialer.RxQueueSize = cfg.RxQueueSize
	dialer.TxQueueSize = cfg.TxQueueSize
	dialer.RedialBackoffInitial = cfg.RedialBackoffInitial.Duration()
	dialer.RedialBackoffMaximum = cfg.RedialBackoffMaximum.Duration()
	transport, e := dialer.Dial(loc.Network, loc.Local, loc.Remote)
	if e != nil {
		return nil, e
	}

	return Wrap(transport, cfg)
}

// Wrap wraps a sockettransport.Transport to a socket face.
func Wrap(transport sockettransport.Transport, cfg Config) (iface.Face, error) {
	face := &socketFace{
		transport: transport,
		rxMempool: ndni.PacketMempool.Get(eal.NumaSocket{}),
	}
	return iface.New(iface.NewParams{
		Config: cfg.Config,
		Init: func(f iface.Face) (iface.InitResult, error) {
			face.Face = f
			return iface.InitResult{
				Face:      face,
				L2TxBurst: C.go_SocketFace_TxBurst,
			}, nil
		},
		Start: func() error {
			face.transport.OnStateChange(func(st l3.TransportState) {
				face.SetDown(st != l3.TransportUp)
			})

			if e := rxg.addFace(cfg.RxGroupCapacity); e != nil {
				return e
			}
			go face.rxLoop()
			iface.ActivateTxFace(face)
			return nil
		},
		Locator: func() iface.Locator {
			conn := face.transport.Conn()
			laddr, raddr := conn.LocalAddr(), conn.RemoteAddr()

			var loc Locator
			loc.Network = raddr.Network()
			loc.Remote = raddr.String()
			if laddr != nil {
				loc.Local = laddr.String()
			}
			return loc
		},
		Stop: func() error {
			rxg.removeFace()
			iface.DeactivateTxFace(face)
			return nil
		},
		Close: func() error {
			// close the channel after Get(id) would return nil.
			// Otherwise, go_SocketFace_TxBurst could panic for sending into closed channel.
			close(face.transport.Tx())
			return nil
		},
		ReadExCounters: func() interface{} {
			return face.transport.Counters()
		},
	})
}

// socketFace is a face using socket as transport.
type socketFace struct {
	iface.Face
	transport sockettransport.Transport
	rxMempool *pktmbuf.Pool
}

func (face *socketFace) ptr() *C.Face {
	return (*C.Face)(face.Ptr())
}

func (face *socketFace) rxLoop() {
	for wire := range face.transport.Rx() {
		vec, e := face.rxMempool.Alloc(1)
		if e != nil { // ignore alloc error
			continue
		}

		mbuf := vec[0]
		mbuf.SetPort(uint16(face.ID()))
		mbuf.SetTimestamp(eal.TscNow())
		mbuf.SetHeadroom(0)
		mbuf.Append(wire)

		rxg.rx(vec)
	}
}

//export go_SocketFace_TxBurst
func go_SocketFace_TxBurst(faceC *C.Face, pkts **C.struct_rte_mbuf, nPkts C.uint16_t) C.uint16_t {
	face := iface.Get(iface.ID(faceC.id)).(*socketFace)
	vec := pktmbuf.VectorFromPtr(unsafe.Pointer(pkts), int(nPkts))
	defer vec.Close()
	for _, pkt := range vec {
		wire := pkt.Bytes()
		select {
		case face.transport.Tx() <- wire:
		default: // packet loss
		}
	}
	return nPkts
}
