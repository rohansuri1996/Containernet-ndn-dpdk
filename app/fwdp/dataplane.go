// Package fwdp implements the forwarder's data plane.
package fwdp

import (
	"fmt"
	"math/rand"

	"github.com/pkg/math"
	"github.com/usnistgov/ndn-dpdk/container/fib"
	"github.com/usnistgov/ndn-dpdk/container/fib/fibdef"
	"github.com/usnistgov/ndn-dpdk/container/ndt"
	"github.com/usnistgov/ndn-dpdk/container/pcct"
	"github.com/usnistgov/ndn-dpdk/container/pit"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealthread"
	"github.com/usnistgov/ndn-dpdk/iface"
	"go.uber.org/multierr"
	"go4.org/must"
)

// Thread roles.
const (
	RoleInput  = iface.RoleRx
	RoleOutput = iface.RoleTx
	RoleCrypto = "CRYPTO"
	RoleFwd    = "FWD"
)

// Config contains data plane configuration.
type Config struct {
	LCoreAlloc ealthread.Config `json:"-"`

	Ndt      ndt.Config         `json:"ndt,omitempty"`
	Fib      fibdef.Config      `json:"fib,omitempty"`
	Pcct     pcct.Config        `json:"pcct,omitempty"`
	Suppress pit.SuppressConfig `json:"suppress,omitempty"`

	Crypto            CryptoConfig         `json:"crypto,omitempty"`
	FwdInterestQueue  iface.PktQueueConfig `json:"fwdInterestQueue,omitempty"`
	FwdDataQueue      iface.PktQueueConfig `json:"fwdDataQueue,omitempty"`
	FwdNackQueue      iface.PktQueueConfig `json:"fwdNackQueue,omitempty"`
	LatencySampleFreq *int                 `json:"latencySampleFreq,omitempty"` // latency sample frequency, between 0 and 30
}

func (cfg *Config) validate() error {
	if len(cfg.LCoreAlloc) > 0 {
		if e := cfg.LCoreAlloc.ValidateRoles(map[string]int{RoleInput: 1, RoleOutput: 1, RoleCrypto: 0, RoleFwd: 1}); e != nil {
			return e
		}
	}

	if cfg.FwdDataQueue.DequeueBurstSize <= 0 {
		cfg.FwdDataQueue.DequeueBurstSize = iface.MaxBurstSize
	}
	if cfg.FwdNackQueue.DequeueBurstSize <= 0 {
		cfg.FwdNackQueue.DequeueBurstSize = cfg.FwdDataQueue.DequeueBurstSize
	}
	if cfg.FwdInterestQueue.DequeueBurstSize <= 0 {
		cfg.FwdInterestQueue.DequeueBurstSize = math.MaxInt(cfg.FwdDataQueue.DequeueBurstSize/2, 1)
	}

	latencySampleFreq := 16
	if cfg.LatencySampleFreq != nil {
		latencySampleFreq = math.MinInt(math.MaxInt(0, *cfg.LatencySampleFreq), 30)
	}
	cfg.LatencySampleFreq = &latencySampleFreq

	return nil
}

// DefaultAlloc is the default lcore allocation algorithm.
func DefaultAlloc() (m map[string]eal.LCores, e error) {
	m = map[string]eal.LCores{}
	tryAlloc := func(reqs []ealthread.AllocReq) error {
		lc, e := ealthread.AllocRequest(reqs...)
		if e != nil {
			return e
		}
		for i, req := range reqs {
			m[req.Role] = append(m[req.Role], lc[i])
		}
		return nil
	}

	reqs := []ealthread.AllocReq{{Role: RoleCrypto}}
	for _, socket := range eal.Sockets {
		reqs = append(reqs,
			ealthread.AllocReq{Role: RoleFwd},
			ealthread.AllocReq{Role: RoleInput, Socket: socket},
			ealthread.AllocReq{Role: RoleOutput, Socket: socket},
		)
	}

	if tryAlloc(reqs) != nil {
		reqs = reqs[:4]
		for i := range reqs {
			reqs[i].Socket = eal.NumaSocket{}
		}
		if e := tryAlloc(reqs); e != nil {
			return nil, e
		}
	}

	reqs = []ealthread.AllocReq{{Role: RoleFwd}, {Role: RoleOutput}, {Role: RoleInput}, {Role: RoleFwd}}
	for {
		if tryAlloc(reqs) == nil {
			continue
		}
		if len(reqs) == 1 {
			break
		}
		reqs = reqs[1:]
	}

	return m, nil
}

// DataPlane represents the forwarder data plane.
type DataPlane struct {
	ndt   *ndt.Ndt
	fib   *fib.Fib
	fwis  []*Input
	fwcs  []*Crypto
	fwcsh map[eal.NumaSocket]*CryptoShared
	fwds  []*Fwd
}

// New creates and launches forwarder data plane.
func New(cfg Config) (dp *DataPlane, e error) {
	if e := cfg.validate(); e != nil {
		return nil, e
	}
	dp = &DataPlane{}

	var alloc map[string]eal.LCores
	if len(cfg.LCoreAlloc) > 0 {
		alloc, e = ealthread.AllocConfig(cfg.LCoreAlloc)
	} else {
		alloc, e = DefaultAlloc()
	}
	if e != nil {
		return nil, e
	}
	lcRx, lcTx, lcCrypto, lcFwd := alloc[RoleInput], alloc[RoleOutput], alloc[RoleCrypto], alloc[RoleFwd]

	{
		ndtSockets := []eal.NumaSocket{}
		for _, lc := range lcRx {
			ndtSockets = append(ndtSockets, lc.NumaSocket())
		}
		for _, lc := range lcCrypto {
			ndtSockets = append(ndtSockets, lc.NumaSocket())
		}
		dp.ndt = ndt.New(cfg.Ndt, ndtSockets)
		dp.ndt.Randomize(len(lcFwd))
	}

	for _, lc := range lcTx {
		txl := iface.NewTxLoop(lc.NumaSocket())
		txl.SetLCore(lc)
		ealthread.Launch(txl)
	}

	var fibFwds []fib.LookupThread
	for i, lc := range lcFwd {
		fwd := newFwd(i)
		if e = fwd.Init(lc, cfg.Pcct, cfg.FwdInterestQueue, cfg.FwdDataQueue, cfg.FwdNackQueue,
			*cfg.LatencySampleFreq, cfg.Suppress); e != nil {
			must.Close(dp)
			return nil, fmt.Errorf("Fwd[%d].Init(): %w", i, e)
		}
		dp.fwds = append(dp.fwds, fwd)
		fibFwds = append(fibFwds, fwd)
	}

	if dp.fib, e = fib.New(cfg.Fib, fibFwds); e != nil {
		must.Close(dp)
		return nil, fmt.Errorf("fib.New: %w", e)
	}

	fwcshList := []*CryptoShared{}
	{
		dp.fwcsh = map[eal.NumaSocket]*CryptoShared{}
		fwcID := len(dp.fwis)
		for socket, lcs := range lcCrypto.ByNumaSocket() {
			socketFwcs := []*Crypto{}
			for _, lc := range lcs {
				fwc := newCrypto(fwcID)
				if e = fwc.Init(lc, dp.ndt, dp.fwds); e != nil {
					must.Close(dp)
					return nil, fmt.Errorf("Crypto[%d].Init(): %w", fwcID, e)
				}
				socketFwcs = append(socketFwcs, fwc)
				dp.fwcs = append(dp.fwcs, fwc)
				fwcID++
			}

			fwcsh, e := newCryptoShared(cfg.Crypto, socket, len(socketFwcs))
			if e != nil {
				must.Close(dp)
				return nil, fmt.Errorf("newCryptoShared[%s]: %w", socket, e)
			}
			fwcsh.AssignTo(socketFwcs)
			fwcshList = append(fwcshList, fwcsh)
			dp.fwcsh[socket] = fwcsh
		}
	}

	for _, fwc := range dp.fwcs {
		ealthread.Launch(fwc)
	}
	for _, fwd := range dp.fwds {
		if fwcsh := dp.fwcsh[fwd.NumaSocket()]; fwcsh != nil {
			fwcsh.ConnectTo(fwd)
		} else if n := len(fwcshList); n > 0 {
			fwcshList[rand.Intn(n)].ConnectTo(fwd)
		}
		ealthread.Launch(fwd)
	}

	for i, lc := range lcRx {
		fwi := newInput(i)
		if e = fwi.Init(lc, dp.ndt, dp.fwds); e != nil {
			must.Close(dp)
			return nil, fmt.Errorf("Input[%d].Init(): %w", i, e)
		}
		dp.fwis = append(dp.fwis, fwi)
		ealthread.Launch(fwi.rxl)
	}

	return dp, nil
}

// Ndt returns the NDT.
func (dp *DataPlane) Ndt() *ndt.Ndt {
	return dp.ndt
}

// Fib returns the FIB.
func (dp *DataPlane) Fib() *fib.Fib {
	return dp.fib
}

// Fwds returns a list of forwarding threads.
func (dp *DataPlane) Fwds() []*Fwd {
	return dp.fwds
}

// Close stops the data plane and releases resources.
func (dp *DataPlane) Close() error {
	var lcores eal.LCores
	errs := []error{}

	for _, rxl := range iface.ListRxLoops() {
		lcores = append(lcores, rxl.LCore())
	}
	for _, txl := range iface.ListTxLoops() {
		lcores = append(lcores, txl.LCore())
	}

	errs = append(errs, iface.CloseAll())
	for _, fwc := range dp.fwcs {
		lcores = append(lcores, fwc.LCore())
		errs = append(errs, fwc.Close())
	}
	for _, fwcsh := range dp.fwcsh {
		errs = append(errs, fwcsh.Close())
	}
	for _, fwd := range dp.fwds {
		lcores = append(lcores, fwd.LCore())
		errs = append(errs, fwd.Close())
	}
	for _, fwi := range dp.fwis {
		errs = append(errs, fwi.Close())
	}
	if dp.fib != nil {
		errs = append(errs, dp.fib.Close())
	}
	if dp.ndt != nil {
		errs = append(errs, dp.ndt.Close())
	}

	ealthread.AllocFree(lcores...)
	return multierr.Combine(errs...)
}
