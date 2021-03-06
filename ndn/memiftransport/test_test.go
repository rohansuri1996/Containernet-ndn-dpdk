package memiftransport_test

import (
	"os"
	"testing"

	"github.com/usnistgov/ndn-dpdk/core/testenv"
)

var (
	makeAR = testenv.MakeAR
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == memifbridgeArg {
		memifbridgeHelper()
		os.Exit(0)
	}

	testenv.Exit(m.Run())
}
