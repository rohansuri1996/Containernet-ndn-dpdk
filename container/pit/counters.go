package pit

import (
	"fmt"

	"github.com/graphql-go/graphql"
	"github.com/usnistgov/ndn-dpdk/core/gqlserver"
)

// Counters contains PIT counters.
type Counters struct {
	NEntries  uint64 `json:"nEntries" gqldesc:"Current number of entries."`
	NInsert   uint64 `json:"nInsert" gqldesc:"Insertions that created a new PIT entry."`
	NFound    uint64 `json:"nFound" gqldesc:"Insertions that found an existing PIT entry."`
	NCsMatch  uint64 `json:"nCsMatch" gqldesc:"Insertions that matched a CS entry."`
	NAllocErr uint64 `json:"nAllocErr" gqldesc:"Insertions that failed due to allocation error."`
	NDataHit  uint64 `json:"nDataHit" gqldesc:"Lookup-by-Data operations that found PIT entry/entries."`
	NDataMiss uint64 `json:"nDataMiss" gqldesc:"Lookup-by-Data operations that did not find PIT entry."`
	NNackHit  uint64 `json:"nNackHit" gqldesc:"Lookup-by-Nack operations that found PIT entry."`
	NNackMiss uint64 `json:"nNackMiss" gqldesc:"Lookup-by-Nack operations that did not found PIT entry."`
	NExpired  uint64 `json:"nExpired" gqldesc:"Entries expired."`
}

func (cnt Counters) String() string {
	return fmt.Sprintf("%d entries, %d inserts, %d found, %d cs-match, %d alloc-err, "+
		"%d data-hit, %d data-miss, %d nack-hit, %d nack-miss, %d expired",
		cnt.NEntries, cnt.NInsert, cnt.NFound, cnt.NCsMatch, cnt.NAllocErr,
		cnt.NDataHit, cnt.NDataMiss, cnt.NNackHit, cnt.NNackMiss, cnt.NExpired)
}

// Counters reads counters from this PIT.
func (pit *Pit) Counters() (cnt Counters) {
	cnt.NEntries = uint64(pit.nEntries)
	cnt.NInsert = uint64(pit.nInsert)
	cnt.NFound = uint64(pit.nFound)
	cnt.NCsMatch = uint64(pit.nCsMatch)
	cnt.NAllocErr = uint64(pit.nAllocErr)
	cnt.NDataHit = uint64(pit.nDataHit)
	cnt.NDataMiss = uint64(pit.nDataMiss)
	cnt.NNackHit = uint64(pit.nNackHit)
	cnt.NNackMiss = uint64(pit.nNackMiss)
	cnt.NExpired = uint64(pit.timeoutSched.nTriggered)
	return cnt
}

// GqlCountersType is the GraphQL type for Counters.
var GqlCountersType = graphql.NewObject(graphql.ObjectConfig{
	Name:   "PitCounters",
	Fields: gqlserver.BindFields(Counters{}, nil),
})
