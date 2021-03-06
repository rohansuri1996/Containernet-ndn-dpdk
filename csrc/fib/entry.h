#ifndef NDNDPDK_FIB_ENTRY_H
#define NDNDPDK_FIB_ENTRY_H

/** @file */

#include "../core/urcu.h"
#include "../iface/faceid.h"
#include "../strategycode/strategy-code.h"
#include "enum.h"
#include <urcu/rculfhash.h>

typedef struct FibEntryDyn
{
  uint32_t nRxInterests;
  uint32_t nRxData;
  uint32_t nRxNacks;
  uint32_t nTxInterests;
  char pad_[16];
  char scratch[FibScratchSize];
} FibEntryDyn;
static_assert(sizeof(FibEntryDyn) % RTE_CACHE_LINE_SIZE == 0, "");

typedef struct FibEntry FibEntry;

/** @brief A FIB entry. */
struct FibEntry
{
  struct cds_lfht_node lfhtnode;
  uint16_t nameL; ///< TLV-LENGTH of name
  uint8_t nameV[FibMaxNameLength];
  char cachelineA_[0];

  union
  {
    /**
     * @brief Forwarding strategy.
     * @pre height == 0
     */
    StrategyCode* strategy;

    /**
     * @brief Real FIB entry.
     * @pre height > 0
     */
    FibEntry* realEntry;
  };

  uint32_t seqNum; ///< sequence number to detect FIB changes

  uint8_t nComps;    ///< number of name components
  uint8_t nNexthops; ///< number of nexthops

  /**
   * @brief Height of a virtual node.
   * @pre nComps == startDepth and this is a virtual node
   *
   * This field is known as '(MD - M)' in 2-stage LPM algorithm.
   * The height of a node is the length of the longest downward path to a leaf from that node.
   */
  uint8_t height;

  FaceID nexthops[FibMaxNexthops];

  char padB_[32];
  char cachelineB_[0];
  FibEntryDyn dyn[0];
};
static_assert(offsetof(FibEntry, cachelineA_) % RTE_CACHE_LINE_SIZE == 0, "");
static_assert(offsetof(FibEntry, cachelineB_) % RTE_CACHE_LINE_SIZE == 0, "");

// FibEntry.nComps must be able to represent maximum number of name components that
// can fit in FibMaxNameLength octets.
static_assert(UINT8_MAX >= FibMaxNameLength / 2, "");

static inline FibEntry*
FibEntry_GetReal(FibEntry* entry)
{
  if (unlikely(entry == NULL) || likely(entry->height == 0)) {
    return entry;
  }
  return entry->realEntry;
}

static inline FibEntry**
FibEntry_PtrRealEntry(FibEntry* entry)
{
  return &entry->realEntry;
}

static inline StrategyCode**
FibEntry_PtrStrategy(FibEntry* entry)
{
  return &entry->strategy;
}

static inline FibEntryDyn*
FibEntry_PtrDyn(FibEntry* entry, int index)
{
  return &entry->dyn[index];
}

#endif // NDNDPDK_FIB_ENTRY_H
