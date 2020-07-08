package ndt_test

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/usnistgov/ndn-dpdk/container/ndt"
	"github.com/usnistgov/ndn-dpdk/core/cptr"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealthread"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type lookupTestEntry struct {
	Name    ndn.Name
	Results []uint8
}

type lookupTestThread struct {
	ealthread.Thread
	stop    ealthread.StopChan
	ndtt    *ndt.Thread
	Entries []lookupTestEntry
}

func newNdtLookupTestThread(ndt *ndt.Ndt, threadIndex int, names []ndn.Name) *lookupTestThread {
	th := &lookupTestThread{
		stop: ealthread.NewStopChan(),
		ndtt: ndt.GetThread(threadIndex),
	}
	for _, name := range names {
		th.Entries = append(th.Entries, lookupTestEntry{name, nil})
	}
	th.Thread = ealthread.New(cptr.Func0.Void(th.main), th.stop)
	return th
}

func (th *lookupTestThread) ThreadRole() string {
	return "TEST"
}

func (th *lookupTestThread) main() {
	for th.stop.Continue() {
		i := rand.Intn(len(th.Entries))
		entry := &th.Entries[i]
		result := th.ndtt.Lookup(entry.Name)
		if len(entry.Results) == 0 || entry.Results[len(entry.Results)-1] != result {
			entry.Results = append(entry.Results, result)
		}
	}
}

func TestNdt(t *testing.T) {
	assert, require := makeAR(t)

	cfg := ndt.Config{
		PrefixLen:  2,
		IndexBits:  8,
		SampleFreq: 2,
	}
	ndt := ndt.New(cfg, make([]eal.NumaSocket, 4))
	defer ndt.Close()

	nameStrs := []string{
		"/",
		"/...",
		"/A/2=C",
		"/A/A/C",
		"/A/A/D",
		"/B",
		"/B/2=C",
		"/B/C",
	}
	names := make([]ndn.Name, len(nameStrs))
	nameIndices := make(map[uint64]bool)
	for i, nameStr := range nameStrs {
		names[i] = ndn.ParseName(nameStr)
		nameIndices[ndt.IndexOfName(names[i])] = true
	}
	assert.Len(nameIndices, 7)

	threads := []*lookupTestThread{
		newNdtLookupTestThread(ndt, 0, names[:6]),
		newNdtLookupTestThread(ndt, 1, names[:6]),
		newNdtLookupTestThread(ndt, 2, names[:6]),
		newNdtLookupTestThread(ndt, 3, names[6:]),
	}

	ndt.Randomize(250)
	cnt0 := ndt.ReadCounters()
	for _, th := range threads {
		require.NoError(ealthread.Launch(th))
	}

	time.Sleep(10 * time.Millisecond)
	cnt1 := ndt.ReadCounters()
	ndt.Randomize(250)
	time.Sleep(10 * time.Millisecond)

	for _, th := range threads {
		th.Stop()
	}
	time.Sleep(10 * time.Millisecond)
	cnt2 := ndt.ReadCounters()

	// all counters are zero initially
	require.Len(cnt0, 256)
	sort.Ints(cnt0)
	assert.Zero(cnt0[255])

	// each name has one or two results
	for j, th := range threads {
		for i, entry := range th.Entries {
			assert.True(len(entry.Results) == 1 || len(entry.Results) == 2, "threads[%d].Entries[%d].Results len=%d", j, i, len(entry.Results))
		}
	}

	// th0, th1, th2 should have consistent results
	for i := range names[:6] {
		for j := 1; j <= 2; j++ {
			assert.Equal(threads[0].Entries[i].Results, threads[j].Entries[i].Results)
		}
	}

	// /A/A/C and /A/A/D should have same results
	assert.Equal(threads[0].Entries[3].Results, threads[0].Entries[4].Results)

	verifyCnt := func(cnt []int) {
		require.Len(cnt, 256)
		for i, n := range cnt {
			if nameIndices[uint64(i)] {
				assert.NotZero(n)
			} else {
				assert.Zero(n)
			}
		}
	}
	verifyCnt(cnt1)
	verifyCnt(cnt2)
}
