package ealtest

import (
	"testing"

	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
)

func TestJoinDevArgs(t *testing.T) {
	assert, _ := makeAR(t)

	assert.Equal("", eal.JoinDevArgs(nil))
	assert.Equal("", eal.JoinDevArgs(map[string]interface{}{}))

	assert.Contains([]string{"a=-1,B=str", "B=str,a=-1"},
		eal.JoinDevArgs(map[string]interface{}{"a": -1, "B": "str"}))

	assert.Equal("override", eal.JoinDevArgs(map[string]interface{}{
		"":  "override",
		"a": -1,
		"B": "ignored",
	}))
}
