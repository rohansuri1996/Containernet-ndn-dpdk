package fwdp

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/graphql-go/graphql"
	"github.com/usnistgov/ndn-dpdk/container/cs/cscnt"
	"github.com/usnistgov/ndn-dpdk/container/pit"
	"github.com/usnistgov/ndn-dpdk/core/gqlserver"
	"github.com/usnistgov/ndn-dpdk/core/runningstat"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealthread"
	"github.com/usnistgov/ndn-dpdk/iface"
)

var (
	// GqlDataPlane is the DataPlane instance accessible via GraphQL.
	GqlDataPlane *DataPlane

	errNoGqlDataPlane = errors.New("DataPlane unavailable")
)

// GraphQL types.
var (
	GqlInputNodeType   *gqlserver.NodeType
	GqlInputType       *graphql.Object
	GqlFwdCountersType *graphql.Object
	GqlFwdNodeType     *gqlserver.NodeType
	GqlFwdType         *graphql.Object
	GqlDataPlaneType   *graphql.Object
)

func init() {
	GqlInputNodeType = gqlserver.NewNodeType((*Input)(nil))
	GqlInputNodeType.Retrieve = func(id string) (interface{}, error) {
		if GqlDataPlane == nil {
			return nil, errNoGqlDataPlane
		}
		i, e := strconv.Atoi(id)
		if e != nil || i < 0 || i >= len(GqlDataPlane.fwis) {
			return nil, nil
		}
		return GqlDataPlane.fwis[i], nil
	}

	GqlInputType = graphql.NewObject(GqlInputNodeType.Annotate(graphql.ObjectConfig{
		Name: "FwInput",
		Fields: graphql.Fields{
			"nid": &graphql.Field{
				Description: "Input thread index.",
				Type:        gqlserver.NonNullInt,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					input := p.Source.(*Input)
					return input.id, nil
				},
			},
			"worker": ealthread.GqlWithWorker(nil),
		},
	}))
	GqlInputNodeType.Register(GqlInputType)

	GqlFwdCountersType = graphql.NewObject(graphql.ObjectConfig{
		Name: "FwFwdCounters",
		Fields: graphql.Fields{
			"inputLatency": &graphql.Field{
				Description: "Latency between packet arrival and dequeuing at forwarding thread, in nanoseconds.",
				Type:        graphql.NewNonNull(runningstat.GqlSnapshotType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					fwd := p.Source.(*Fwd)
					latencyStat := runningstat.FromPtr(unsafe.Pointer(&fwd.c.latencyStat))
					return latencyStat.Read().Scale(eal.TscNanos), nil
				},
			},
		},
	})
	for fieldName, field := range gqlserver.BindFields(FwdCounters{}, nil) {
		resolveFromCounters := field.Resolve
		field.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
			fwd := p.Source.(*Fwd)
			p.Source = fwd.Counters()
			return resolveFromCounters(p)
		}
		GqlFwdCountersType.AddFieldConfig(fieldName, field)
	}
	defineFwdPktCounter := func(plural string, getDemux func(iface.RxLoop) *iface.InputDemux) {
		GqlFwdCountersType.AddFieldConfig(fmt.Sprintf("n%sQueued", plural), &graphql.Field{
			Description: fmt.Sprintf("%s queued in input thread.", plural),
			Type:        gqlserver.NonNullInt,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				index := p.Source.(*Fwd).id
				var sum uint64
				for _, input := range GqlDataPlane.fwis {
					sum += getDemux(input.rxl).DestCounters(index).NQueued
				}
				return sum, nil
			},
		})
		GqlFwdCountersType.AddFieldConfig(fmt.Sprintf("n%sDropped", plural), &graphql.Field{
			Description: fmt.Sprintf("%s dropped in input thread.", plural),
			Type:        gqlserver.NonNullInt,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				index := p.Source.(*Fwd).id
				var sum uint64
				for _, input := range GqlDataPlane.fwis {
					sum += getDemux(input.rxl).DestCounters(index).NDropped
				}
				return sum, nil
			},
		})
		pktQueueF, _ := reflect.TypeOf(Fwd{}).FieldByName("queue" + plural[:1])
		GqlFwdCountersType.AddFieldConfig(fmt.Sprintf("n%sCongMarked", plural), &graphql.Field{
			Description: fmt.Sprintf("Congestion marks added to %s.", plural),
			Type:        gqlserver.NonNullInt,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				fwdV := reflect.ValueOf(p.Source.(*Fwd)).Elem()
				q := fwdV.FieldByIndex(pktQueueF.Index).Interface().(*iface.PktQueue)
				return int(q.Counters().NDrops), nil
			},
		})
	}
	defineFwdPktCounter("Interests", iface.RxLoop.InterestDemux)
	defineFwdPktCounter("Data", iface.RxLoop.DataDemux)
	defineFwdPktCounter("Nacks", iface.RxLoop.NackDemux)

	GqlFwdNodeType = gqlserver.NewNodeType((*Fwd)(nil))
	GqlFwdNodeType.Retrieve = func(id string) (interface{}, error) {
		if GqlDataPlane == nil {
			return nil, errNoGqlDataPlane
		}
		i, e := strconv.Atoi(id)
		if e != nil || i < 0 || i >= len(GqlDataPlane.fwds) {
			return nil, nil
		}
		return GqlDataPlane.fwds[i], nil
	}

	GqlFwdType = graphql.NewObject(GqlFwdNodeType.Annotate(graphql.ObjectConfig{
		Name: "FwFwd",
		Fields: graphql.Fields{
			"nid": &graphql.Field{
				Description: "Forwarding thread index.",
				Type:        gqlserver.NonNullInt,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					fwd := p.Source.(*Fwd)
					return fwd.id, nil
				},
			},
			"worker": ealthread.GqlWithWorker(nil),
			"counters": &graphql.Field{
				Description: "Forwarding counters.",
				Type:        graphql.NewNonNull(GqlFwdCountersType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return p.Source, nil
				},
			},
			"pitCounters": &graphql.Field{
				Description: "PIT counters.",
				Type:        graphql.NewNonNull(pit.GqlCountersType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					fwd := p.Source.(*Fwd)
					return fwd.Pit().Counters(), nil
				},
			},
			"csCounters": &graphql.Field{
				Description: "CS counters.",
				Type:        graphql.NewNonNull(cscnt.GqlCountersType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					fwd := p.Source.(*Fwd)
					return cscnt.ReadCounters(fwd.Pit(), fwd.Cs()), nil
				},
			},
		},
	}))
	GqlFwdNodeType.Register(GqlFwdType)

	GqlDataPlaneType = graphql.NewObject(graphql.ObjectConfig{
		Name: "FwDataPlane",
		Fields: graphql.Fields{
			"inputs": &graphql.Field{
				Description: "Input threads.",
				Type:        gqlserver.NewNonNullList(GqlInputType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					dp := p.Source.(*DataPlane)
					return dp.fwis, nil
				},
			},
			"fwds": &graphql.Field{
				Description: "Forwarding threads.",
				Type:        gqlserver.NewNonNullList(GqlFwdType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					dp := p.Source.(*DataPlane)
					return dp.fwds, nil
				},
			},
		},
	})

	gqlserver.AddQuery(&graphql.Field{
		Name:        "fwdp",
		Description: "Forwarder data plane.",
		Type:        GqlDataPlaneType,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			return GqlDataPlane, nil
		},
	})
}
