package eal

/*
#include "../../csrc/core/common.h"
*/
import "C"
import (
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/graphql-go/graphql"
	"github.com/usnistgov/ndn-dpdk/core/gqlserver"
	"go.uber.org/zap"
)

// NumaSocket represents a NUMA socket.
// Zero value is SOCKET_ID_ANY.
type NumaSocket struct {
	v int // socket ID + 1
}

// NumaSocketFromID converts socket ID to NumaSocket.
func NumaSocketFromID(id int) (socket NumaSocket) {
	if id < 0 || id >= C.RTE_MAX_NUMA_NODES {
		return socket
	}
	socket.v = id + 1
	return socket
}

// ID returns NUMA socket ID.
func (socket NumaSocket) ID() int {
	return socket.v - 1
}

// IsAny returns true if this represents SOCKET_ID_ANY.
func (socket NumaSocket) IsAny() bool {
	return socket.v == 0
}

// Match returns true if either NumaSocket is SOCKET_ID_ANY, or both are the same NumaSocket.
func (socket NumaSocket) Match(other NumaSocket) bool {
	return socket.IsAny() || other.IsAny() || socket.v == other.v
}

func (socket NumaSocket) String() string {
	if socket.IsAny() {
		return "any"
	}
	return strconv.Itoa(socket.ID())
}

// MarshalJSON encodes NUMA socket as number.
// Any is encoded as null.
func (socket NumaSocket) MarshalJSON() ([]byte, error) {
	if socket.IsAny() {
		return json.Marshal(nil)
	}
	return json.Marshal(socket.ID())
}

// ZapField returns a zap.Field for logging.
func (socket NumaSocket) ZapField(key string) zap.Field {
	if socket.IsAny() {
		return zap.String(key, "any")
	}
	return zap.Int(key, socket.ID())
}

// NumaSocket implements WithNumaSocket.
func (socket NumaSocket) NumaSocket() NumaSocket {
	return socket
}

// RewriteAnyNumaSocket provides a function to change "any NUMA socket" to a concrete NUMA socket.
type RewriteAnyNumaSocket int

const (
	// KeepAnyNumaSocket keeps "any NUMA socket" unchanged.
	KeepAnyNumaSocket RewriteAnyNumaSocket = -2 - iota
	// RewriteAnyNumaSocketFirst rewrites "any NUMA socket" to eal.Sockets[0].
	RewriteAnyNumaSocketFirst
	// RewriteAnyNumaSocketRandom rewrites "any NUMA socket" to eal.RandomSocket().
	RewriteAnyNumaSocketRandom
)

// RewriteAnyNumaSocketTo rewrites "any NUMA socket" to the specified NUMA socket.
func RewriteAnyNumaSocketTo(socket NumaSocket) (r RewriteAnyNumaSocket) {
	return RewriteAnyNumaSocket(socket.ID())
}

// Rewrite rewrites the input if it is "any NUMA socket", otherwise keeps it unchanged.
func (r RewriteAnyNumaSocket) Rewrite(socket NumaSocket) NumaSocket {
	if !socket.IsAny() {
		return socket
	}
	switch r {
	case KeepAnyNumaSocket:
		return socket
	case RewriteAnyNumaSocketFirst:
		if len(Sockets) > 0 {
			return Sockets[0]
		}
		return socket
	case RewriteAnyNumaSocketRandom:
		return RandomSocket()
	default:
		return NumaSocketFromID(int(r))
	}
}

// WithNumaSocket interface is implemented by types that have an associated or preferred NUMA socket.
type WithNumaSocket interface {
	NumaSocket() NumaSocket
}

// ClassifyByNumaSocket classifies items by NUMA socket.
//  T: type that satisfies WithNumaSocket interface
//  s: source []T
// Returns map[eal.NumaSocket][]T
func ClassifyByNumaSocket(s interface{}, r RewriteAnyNumaSocket) interface{} {
	sV := reflect.ValueOf(s)
	sT := sV.Type()
	mV := reflect.MakeMap(reflect.MapOf(reflect.TypeOf(NumaSocket{}), sT))

	for i, count := 0, sV.Len(); i < count; i++ {
		itemV := sV.Index(i)

		socket := r.Rewrite(itemV.Interface().(WithNumaSocket).NumaSocket())
		socketV := reflect.ValueOf(socket)

		bucketV := mV.MapIndex(socketV)
		if bucketV.Kind() == 0 {
			bucketV = reflect.MakeSlice(sT, 0, count)
		}
		bucketV = reflect.Append(bucketV, itemV)
		mV.SetMapIndex(socketV, bucketV)
	}

	return mV.Interface()
}

// GqlWithNumaSocket is a GraphQL field for source object that implements WithNumaSocket.
var GqlWithNumaSocket = &graphql.Field{
	Type:        graphql.Int,
	Name:        "numaSocket",
	Description: "NUMA socket.",
	Resolve: func(p graphql.ResolveParams) (interface{}, error) {
		socket := p.Source.(WithNumaSocket).NumaSocket()
		return gqlserver.Optional(socket.ID(), !socket.IsAny()), nil
	},
}
