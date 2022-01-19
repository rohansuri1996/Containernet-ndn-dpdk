// Package pktmbuf contains bindings of DPDK mbuf library.
package pktmbuf

/*
#include "../../csrc/dpdk/mbuf.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/core/logging"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
)

var logger = logging.New("pktmbuf")

// DefaultHeadroom is the default headroom of a mbuf.
const DefaultHeadroom = C.RTE_PKTMBUF_HEADROOM

// Packet represents a packet in mbuf.
type Packet C.struct_rte_mbuf

// Ptr returns *C.struct_rte_mbuf pointer.
func (pkt *Packet) Ptr() unsafe.Pointer {
	return unsafe.Pointer(pkt)
}

func (pkt *Packet) ptr() *C.struct_rte_mbuf {
	return (*C.struct_rte_mbuf)(pkt)
}

// Close releases the mbuf to mempool.
func (pkt *Packet) Close() error {
	C.rte_pktmbuf_free(pkt.ptr())
	return nil
}

// Len returns packet length in octets.
func (pkt *Packet) Len() int {
	return int(pkt.ptr().pkt_len)
}

// Port returns ingress network interface.
func (pkt *Packet) Port() uint16 {
	return uint16(pkt.ptr().port)
}

// SetPort sets ingress network interface.
func (pkt *Packet) SetPort(port uint16) {
	pkt.ptr().port = C.uint16_t(port)
}

// Timestamp returns receive timestamp.
func (pkt *Packet) Timestamp() eal.TscTime {
	return eal.TscTime(C.Mbuf_GetTimestamp(pkt.ptr()))
}

// SetTimestamp sets receive timestamp.
func (pkt *Packet) SetTimestamp(t eal.TscTime) {
	C.Mbuf_SetTimestamp(pkt.ptr(), C.TscTime(t))
}

// Type32 returns 32-bit packet type.
func (pkt *Packet) Type32() uint32 {
	return uint32(*C.Mbuf_PtrPacketType(pkt.ptr()))
}

// SetType32 sets 32-bit packet type.
func (pkt *Packet) SetType32(packetType uint32) {
	*C.Mbuf_PtrPacketType(pkt.ptr()) = C.uint32_t(packetType)
}

// SegmentLengths returns lengths of segments in this packet.
func (pkt *Packet) SegmentLengths() (list []int) {
	for m := pkt.ptr(); m != nil; m = m.next {
		list = append(list, int(m.data_len))
	}
	return list
}

// DataPtr returns void* pointer to data in first segment.
func (pkt *Packet) DataPtr() unsafe.Pointer {
	pktC := pkt.ptr()
	return unsafe.Add(pktC.buf_addr, pktC.data_off)
}

// Bytes returns a []byte that contains a copy of the data in this packet.
func (pkt *Packet) Bytes() []byte {
	b := make([]byte, pkt.Len())
	if len(b) > 0 {
		C.Mbuf_CopyTo(pkt.ptr(), unsafe.Pointer(&b[0]))
	}
	return b
}

// Headroom returns headroom of the first segment.
func (pkt *Packet) Headroom() int {
	return int(C.rte_pktmbuf_headroom(pkt.ptr()))
}

// SetHeadroom changes headroom of the first segment.
// It can only be used on an empty packet.
func (pkt *Packet) SetHeadroom(headroom int) error {
	if pkt.Len() > 0 {
		return errors.New("cannot change headroom of non-empty packet")
	}
	pktC := pkt.ptr()
	if C.uint16_t(headroom) > pktC.buf_len {
		return errors.New("headroom cannot exceed buffer length")
	}
	pktC.data_off = C.uint16_t(headroom)
	return nil
}

// Tailroom returns tailroom of the last segment.
func (pkt *Packet) Tailroom() int {
	return int(C.rte_pktmbuf_tailroom(C.rte_pktmbuf_lastseg(pkt.ptr())))
}

// Prepend prepends to the packet in headroom of the first segment.
func (pkt *Packet) Prepend(input []byte) error {
	count := len(input)
	if count == 0 {
		return nil
	}

	room := C.rte_pktmbuf_prepend(pkt.ptr(), C.uint16_t(count))
	if room == nil {
		return fmt.Errorf("insufficient headroom %d", pkt.Headroom())
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(room)), count), input)
	return nil
}

// Append appends to the packet in tailroom of the last segment.
func (pkt *Packet) Append(input []byte) error {
	count := len(input)
	if count == 0 {
		return nil
	}

	room := C.rte_pktmbuf_append(pkt.ptr(), C.uint16_t(count))
	if room == nil {
		return fmt.Errorf("insufficient tailroom %d", pkt.Tailroom())
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(room)), count), input)
	return nil
}

// Chain combines two packets.
// tail will be freed when pkt is freed.
func (pkt *Packet) Chain(tail *Packet) error {
	pktC := pkt.ptr()
	if ok := C.Mbuf_Chain(pktC, C.rte_pktmbuf_lastseg(pktC), tail.ptr()); !bool(ok) {
		return errors.New("too many segments")
	}
	return nil
}

// PacketFromPtr converts *C.struct_rte_mbuf pointer to Packet.
func PacketFromPtr(ptr unsafe.Pointer) *Packet {
	return (*Packet)(ptr)
}
