#ifndef NDNDPDK_NDNI_COMMON_H
#define NDNDPDK_NDNI_COMMON_H

/** @file */

#include "../core/common.h"

#include "../dpdk/cryptodev.h"
#include "../dpdk/mbuf.h"

#include "an.h"
#include "enum.h"

typedef struct Packet Packet;
typedef struct PInterest PInterest;
typedef struct PData PData;
typedef struct PNack PNack;

/** @brief Mempools for packet modification. */
typedef struct PacketMempools
{
  struct rte_mempool* packet;
  struct rte_mempool* indirect;
  struct rte_mempool* header;
} PacketMempools;

/**
 * @brief mbuf alignment requirements for packet modification.
 *
 * If @c linearize is set to true, a packet modification function should output direct mbufs,
 * copying payload when necessary. @c mbuf.data_len cannot exceed @c fragmentPayloadSize .
 * Each mbuf will be transmitted as a NDNLPv2 fragment.
 *
 * If @c linearize is set to false, a packet modification function should use indirect mbufs,
 * and @c fragmentPayloadSize is ignored. TxProc will perform fragmentation when necessary.
 */
typedef struct PacketTxAlign
{
  /** @brief max payload size per fragment. */
  uint16_t fragmentPayloadSize;

  /** @brief whether mbuf must be linearized into consecutive mbuf. */
  bool linearize;
} PacketTxAlign;

#endif // NDNDPDK_NDNI_COMMON_H
