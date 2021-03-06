#ifndef NDNDPDK_IFACE_PKTQUEUE_H
#define NDNDPDK_IFACE_PKTQUEUE_H

/** @file */

#include "../dpdk/mbuf.h"
#include "common.h"

/** @brief A packet queue with simplified CoDel algorithm. */
typedef struct PktQueue PktQueue;

typedef struct PktQueuePopResult
{
  uint32_t count; ///< number of dequeued packets
  bool drop;      ///< whether the first packet should be dropped/ECN-marked
} PktQueuePopResult;

typedef PktQueuePopResult (*PktQueue_PopOp)(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count,
                                            TscTime now);

struct PktQueue
{
  struct rte_ring* ring;

  PktQueue_PopOp pop;
  TscDuration target;
  TscDuration interval;
  uint32_t dequeueBurstSize;

  uint32_t count;
  uint32_t lastCount;
  bool dropping;
  uint16_t recInvSqrt;
  TscTime firstAboveTime;
  TscTime dropNext;
  TscDuration sojourn;

  uint64_t nDrops;
};

/**
 * @brief Enqueue a burst of packets.
 * @param pkts packets with timestamp already set.
 * @return number of rejected packets; they have been freed.
 */
__attribute__((nonnull)) static inline uint32_t
PktQueue_PushPlain(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count)
{
  return Mbuf_EnqueueVector(pkts, count, q->ring);
}

/**
 * @brief Set timestamp on a burst of packets and enqueue them.
 * @return number of rejected packets; they have been freed.
 */
__attribute__((nonnull)) static inline uint32_t
PktQueue_Push(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count, TscTime now)
{
  for (uint32_t i = 0; i < count; ++i) {
    Mbuf_SetTimestamp(pkts[i], now);
  }
  return PktQueue_PushPlain(q, pkts, count);
}

/** @brief Dequeue a burst of packets. */
__attribute__((nonnull)) static inline PktQueuePopResult
PktQueue_Pop(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count, TscTime now)
{
  return (*q->pop)(q, pkts, count, now);
}

__attribute__((nonnull)) PktQueuePopResult
PktQueue_PopPlain(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count, TscTime now);

__attribute__((nonnull)) PktQueuePopResult
PktQueue_PopDelay(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count, TscTime now);

__attribute__((nonnull)) PktQueuePopResult
PktQueue_PopCoDel(PktQueue* q, struct rte_mbuf* pkts[], uint32_t count, TscTime now);

#endif // NDNDPDK_IFACE_PKTQUEUE_H
