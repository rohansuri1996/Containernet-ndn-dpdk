#include "reassembler.h"
#include <rte_hash.h>
#include <rte_jhash.h>

__attribute__((nonnull)) static uint32_t
Reassembler_Hash_(const void* key, uint32_t keyLen, uint32_t initVal)
{
  NDNDPDK_ASSERT(keyLen == sizeof(uint32_t) * 2);
  const uint32_t* words = (const uint32_t*)key;
  return rte_jhash_2words(words[0], words[1], initVal);
}

bool
Reassembler_New(Reassembler* reass, const char* id, uint32_t capacity, unsigned numaSocket)
{
  assert(capacity >= MinReassemblerCapacity && capacity <= MaxReassemblerCapacity);

  struct rte_hash_parameters htParams = {
    .name = id,
    .entries = capacity * 2, // keep occupancy under 50%
    .key_len = sizeof(uint64_t),
    .hash_func = Reassembler_Hash_,
    .socket_id = numaSocket,
  };
  reass->table = rte_hash_create(&htParams);
  if (unlikely(reass->table == NULL)) {
    return false;
  }

  TAILQ_INIT(&reass->list);
  reass->capacity = capacity;
  return true;
}

void
Reassembler_Close(Reassembler* reass)
{
  if (reass->table == NULL) {
    return;
  }

  rte_hash_free(reass->table);
  reass->table = NULL;

  while (!TAILQ_EMPTY(&reass->list)) {
    LpL2* pm = TAILQ_FIRST(&reass->list);
    TAILQ_REMOVE(&reass->list, pm, reassNode);
    rte_pktmbuf_free_bulk_((struct rte_mbuf**)pm->reassFrags, pm->fragCount);
    // unsafe to use TAILQ_FOREACH because pm is being freed
  }
}

__attribute__((nonnull)) static void
Reassembler_Delete_(Reassembler* reass, LpL2* pm, hash_sig_t hash)
{
  int32_t res = rte_hash_del_key_with_hash(reass->table, &pm->seqNumBase, hash);
  NDNDPDK_ASSERT(res >= 0);

  TAILQ_REMOVE(&reass->list, pm, reassNode);
  --reass->count;
}

__attribute__((nonnull)) static void
Reassembler_Drop_(Reassembler* reass, LpL2* pm, hash_sig_t hash)
{
  Reassembler_Delete_(reass, pm, hash);

  reass->nDropFragments += pm->fragCount - __builtin_popcount(pm->reassBitmap);
  rte_pktmbuf_free_bulk_((struct rte_mbuf**)pm->reassFrags, pm->fragCount);
}

__attribute__((nonnull)) static void
Reassembler_Insert_(Reassembler* reass, Packet* fragment, LpL2* pm, hash_sig_t hash)
{
  pm->reassBitmap = (1 << pm->fragCount) - 1;
  pm->reassBitmap &= ~(1 << pm->fragIndex);
  pm->reassFrags[pm->fragIndex] = fragment;

  if (unlikely(reass->count >= reass->capacity)) {
    LpL2* evict = TAILQ_FIRST(&reass->list);
    Reassembler_Drop_(reass, evict, rte_hash_hash(reass->table, &evict->seqNumBase));
  }

  int32_t res = rte_hash_add_key_with_hash_data(reass->table, &pm->seqNumBase, hash, pm);
  if (unlikely(res != 0)) {
    ++reass->nDropFragments;
    rte_pktmbuf_free(Packet_ToMbuf(fragment));
  }

  TAILQ_INSERT_TAIL(&reass->list, pm, reassNode);
  ++reass->count;
}

__attribute__((nonnull, returns_nonnull)) static Packet*
Reassembler_Reassemble_(Reassembler* reass, LpL2* pm, hash_sig_t hash)
{
  static_assert(LpMaxFragments <= RTE_MBUF_MAX_NB_SEGS, "");
  Reassembler_Delete_(reass, pm, hash);

  struct rte_mbuf** pkts = (struct rte_mbuf**)pm->reassFrags;
  for (uint8_t i = 1; i < pm->fragCount; ++i) {
    bool ok = Mbuf_Chain(pkts[0], pkts[i - 1], pkts[i]);
    NDNDPDK_ASSERT(ok);
    // because rte_pktmbuf_is_contiguous(fragment) && LpMaxFragments <= RTE_MBUF_MAX_NB_SEGS
  }

  ++reass->nDeliverPackets;
  reass->nDeliverFragments += pm->fragCount;
  return pm->reassFrags[0];
}

Packet*
Reassembler_Accept(Reassembler* reass, Packet* fragment)
{
  struct rte_mbuf* pkt = Packet_ToMbuf(fragment);
  LpL2* l2 = &Packet_GetLpHdr(fragment)->l2;
  NDNDPDK_ASSERT(l2->fragCount > 1 && // single fragment packets should bypass reassembler
                 l2->fragCount <= LpMaxFragments && RTE_MBUF_DIRECT(pkt) &&
                 rte_pktmbuf_is_contiguous(pkt) && rte_mbuf_refcnt_read(pkt) == 1);

  hash_sig_t hash = rte_hash_hash(reass->table, &l2->seqNumBase);
  LpL2* pm = NULL;
  int res = rte_hash_lookup_with_hash_data(reass->table, &l2->seqNumBase, hash, (void**)&pm);
  if (res < 0) {
    Reassembler_Insert_(reass, fragment, l2, hash);
    return NULL;
  }

  if (unlikely(pm->fragCount != l2->fragCount)) { // FragCount changed
    Reassembler_Drop_(reass, pm, hash);
    rte_pktmbuf_free(pkt);
    ++reass->nDropFragments;
    return NULL;
  }

  uint32_t indexBit = 1 << l2->fragIndex;
  if (unlikely((pm->reassBitmap & indexBit) == 0)) { // duplicate FragIndex
    rte_pktmbuf_free(pkt);
    ++reass->nDropFragments;
    return NULL;
  }

  pm->reassBitmap &= ~indexBit;
  pm->reassFrags[l2->fragIndex] = fragment;
  if (pm->reassBitmap != 0) { // waiting for more fragments
    TAILQ_REMOVE(&reass->list, pm, reassNode);
    TAILQ_INSERT_TAIL(&reass->list, pm, reassNode);
    return NULL;
  }
  return Reassembler_Reassemble_(reass, pm, hash);
}