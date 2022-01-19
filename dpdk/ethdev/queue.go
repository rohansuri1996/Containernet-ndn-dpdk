package ethdev

/*
#include "../../csrc/dpdk/ethdev.h"
*/
import "C"
import (
	"github.com/usnistgov/ndn-dpdk/dpdk/pktmbuf"
)

// RxQueue represents an RX queue.
type RxQueue struct {
	Port  uint16
	Queue uint16
}

// RxBurst receives a burst of input packets.
// Returns the number of packets received and written into vec.
func (q RxQueue) RxBurst(vec pktmbuf.Vector) int {
	if len(vec) == 0 {
		return 0
	}
	res := C.rte_eth_rx_burst(C.uint16_t(q.Port), C.uint16_t(q.Queue),
		(**C.struct_rte_mbuf)(vec.Ptr()), C.uint16_t(len(vec)))
	return int(res)
}

// TxQueue represents an TX queue.
type TxQueue struct {
	Port  uint16
	Queue uint16
}

// TxBurst transmits a burst of output packets.
// Returns the number of packets enqueued.
func (q TxQueue) TxBurst(vec pktmbuf.Vector) int {
	if len(vec) == 0 {
		return 0
	}
	res := C.rte_eth_tx_burst(C.uint16_t(q.Port), C.uint16_t(q.Queue),
		(**C.struct_rte_mbuf)(vec.Ptr()), C.uint16_t(len(vec)))
	return int(res)
}

func (port ethDev) RxQueues() (list []RxQueue) {
	id, info := uint16(port.ID()), port.DevInfo()
	for queue := uint16(0); queue < info.Nb_rx_queues; queue++ {
		list = append(list, RxQueue{id, queue})
	}
	return list
}

func (port ethDev) TxQueues() (list []TxQueue) {
	id, info := uint16(port.ID()), port.DevInfo()
	for queue := uint16(0); queue < info.Nb_tx_queues; queue++ {
		list = append(list, TxQueue{id, queue})
	}
	return list
}
