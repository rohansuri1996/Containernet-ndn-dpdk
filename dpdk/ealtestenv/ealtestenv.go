// Package ealtestenv initializes EAL for unit testing.
package ealtestenv

import (
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"time"

	mathpkg "github.com/pkg/math"
	"github.com/usnistgov/ndn-dpdk/core/hwinfo"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealconfig"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealinit"
)

// EnvCpus declares an environment variable to reduce the number of CPU cores to use.
// This allows running tests on fewer CPU cores.
const EnvCpus = "EALTESTENV_CPUS"

// WantLCores indicates the number of lcores to be created.
var WantLCores = 6

// UsingThreads is set to true if there are fewer CPU cores than lcores.
var UsingThreads = false

type hwInfoLimitCores struct {
	hwinfo.Provider
	MaxCores int
	cores    hwinfo.Cores
}

func (hwInfo *hwInfoLimitCores) Cores() hwinfo.Cores {
	if len(hwInfo.cores) == 0 {
		cores := hwInfo.Provider.Cores()
		rand.Shuffle(len(cores), reflect.Swapper(cores))
		if len(cores) > hwInfo.MaxCores {
			cores = cores[:hwInfo.MaxCores]
		}
		hwInfo.cores = cores
	}
	return hwInfo.cores
}

// Init initializes EAL for unit testing.
func Init() {
	rand.Seed(time.Now().UnixNano())

	hwInfo := &hwInfoLimitCores{
		Provider: hwinfo.Default,
		MaxCores: WantLCores,
	}
	if maxCores, e := strconv.Atoi(os.Getenv(EnvCpus)); e == nil {
		hwInfo.MaxCores = mathpkg.MinInt(hwInfo.MaxCores, maxCores)
	}

	var cfg ealconfig.Config
	cfg.FilePrefix = "ealtestenv"

	if cores := hwInfo.Cores(); len(cores) < WantLCores {
		cfg.LCoresPerNuma = map[int]int{}
		for socket, numaCores := range cores.ByNumaSocket() {
			cfg.LCoresPerNuma[socket] = int(math.Ceil(float64(WantLCores) * float64(len(numaCores)) / float64(len(cores))))
		}
		UsingThreads = true
	}

	args, e := cfg.Args(hwInfo)
	if e != nil {
		panic(e)
	}
	if e := ealinit.Init(args); e != nil {
		panic(e)
	}
}
