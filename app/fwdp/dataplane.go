package fwdp

/*
#include "input.h"
#include "fwd.h"
*/
import "C"
import (
	"fmt"
	"unsafe"

	"ndn-dpdk/container/fib"
	"ndn-dpdk/container/ndt"
	"ndn-dpdk/container/pcct"
	"ndn-dpdk/core/urcu"
	"ndn-dpdk/dpdk"
	"ndn-dpdk/iface"
)

type Config struct {
	FaceTable iface.FaceTable
	Ndt       ndt.Ndt
	Fib       *fib.Fib

	InputLCores []dpdk.LCore
	FwdLCores   []dpdk.LCore

	FwdQueueCapacity int         // input-fwd queue capacity, must be power of 2
	PcctCfg          pcct.Config // PCCT config, 'Id' and 'NumaSocket' ignored
}

// Forwarder data plane.
type DataPlane struct {
	inputLCores    []dpdk.LCore
	inputs         []*C.FwInput
	inputRxLoopers []iface.IRxLooper
	fwdLCores      []dpdk.LCore
	fwds           []*C.FwFwd
}

func New(cfg Config) (*DataPlane, error) {
	var dp DataPlane
	nInputs := len(cfg.InputLCores)
	nFwds := len(cfg.FwdLCores)
	dp.inputLCores = append([]dpdk.LCore{}, cfg.InputLCores...)
	dp.inputRxLoopers = make([]iface.IRxLooper, nInputs)
	dp.fwdLCores = append([]dpdk.LCore{}, cfg.FwdLCores...)

	ftC := (*C.FaceTable)(cfg.FaceTable.GetPtr())
	ndtC := (*C.Ndt)(cfg.Ndt.GetPtr())
	fibC := (*C.Fib)(cfg.Fib.GetPtr())

	for i, lc := range cfg.FwdLCores {
		queue, e := dpdk.NewRing(fmt.Sprintf("FwFwdQ_%d", i), cfg.FwdQueueCapacity,
			lc.GetNumaSocket(), false, true)
		if e != nil {
			dp.Close()
			return nil, fmt.Errorf("dpdk.NewRing(%d): %v", i, e)
		}

		pcctCfg := cfg.PcctCfg
		pcctCfg.Id = fmt.Sprintf("FwPcct_%d", i)
		pcctCfg.NumaSocket = lc.GetNumaSocket()
		pcct, e := pcct.New(pcctCfg)
		if e != nil {
			queue.Close()
			dp.Close()
			return nil, fmt.Errorf("pcct.New(%d): %v", i, e)
		}

		fwd := (*C.FwFwd)(dpdk.Zmalloc("FwFwd", C.sizeof_FwFwd, lc.GetNumaSocket()))
		fwd.id = C.uint8_t(i)
		fwd.queue = (*C.struct_rte_ring)(queue.GetPtr())

		fwd.ft = ftC
		fwd.fib = fibC
		fwd.pit = (*C.Pit)(pcct.GetPtr())
		fwd.cs = (*C.Cs)(pcct.GetPtr())

		dp.fwds = append(dp.fwds, fwd)
	}

	for i, lc := range cfg.InputLCores {
		fwi := C.FwInput_New(ndtC, C.uint8_t(i), C.uint8_t(nFwds),
			C.unsigned(lc.GetNumaSocket()))
		if fwi == nil {
			dp.Close()
			return nil, dpdk.GetErrno()
		}

		for _, fwd := range dp.fwds {
			C.FwInput_Connect(fwi, fwd)
		}

		dp.inputs = append(dp.inputs, fwi)
	}

	return &dp, nil
}

func (dp *DataPlane) Close() error {
	for _, fwi := range dp.inputs {
		dpdk.Free(fwi)
	}
	for _, fwd := range dp.fwds {
		queue := dpdk.RingFromPtr(unsafe.Pointer(fwd.queue))
		queue.Close()
		pcct := pcct.PcctFromPtr(unsafe.Pointer(fwd.pit))
		pcct.Close()
		dpdk.Free(fwd)
	}
	return nil
}

// Launch input process.
func (dp *DataPlane) LaunchInput(i int, rxl iface.IRxLooper, burstSize int) error {
	lc := dp.inputLCores[i]
	if state := lc.GetState(); state != dpdk.LCORE_STATE_WAIT {
		return fmt.Errorf("lcore %d for input %d is %s", lc, i, lc.GetState())
	}
	dp.inputRxLoopers[i] = rxl
	input := dp.inputs[i]

	ok := lc.RemoteLaunch(func() int {
		rxl.RxLoop(burstSize, unsafe.Pointer(C.FwInput_FaceRx), unsafe.Pointer(input))
		return 0
	})
	if !ok {
		return fmt.Errorf("failed to launch lcore %d for input %d", lc, i)
	}
	return nil
}

// Stop input process.
func (dp *DataPlane) StopInput(i int) {
	if rxl := dp.inputRxLoopers[i]; rxl == nil {
		return
	} else {
		rxl.StopRxLoop()
	}
	dp.inputRxLoopers[i] = nil
	dp.inputLCores[i].Wait()
}

// Launch forwarding process.
func (dp *DataPlane) LaunchFwd(i int) error {
	lc := dp.fwdLCores[i]
	if state := lc.GetState(); state != dpdk.LCORE_STATE_WAIT {
		return fmt.Errorf("lcore %d for fwd %d is %s", lc, i, lc.GetState())
	}
	fwd := dp.fwds[i]
	fwd.stop = C.bool(false)

	ok := lc.RemoteLaunch(func() int {
		rs := urcu.NewReadSide()
		defer rs.Close()
		C.FwFwd_Run(fwd)
		return 0
	})
	if !ok {
		return fmt.Errorf("failed to launch lcore %d for fwd %d", lc, i)
	}
	return nil
}

// Stop forwarding process.
func (dp *DataPlane) StopFwd(i int) {
	dp.fwds[i].stop = C.bool(true)
	dp.fwdLCores[i].Wait()
}
