#include "mintmr.h"

#include "../core/logger.h"

N_LOG_INIT(MinTmr);

MinSched*
MinSched_New(int nSlotBits, TscDuration interval, MinTmrCallback cb, uintptr_t ctx)
{
  uint32_t nSlots = 1 << nSlotBits;
  NDNDPDK_ASSERT(nSlots != 0);

  MinSched* sched = rte_zmalloc("MinSched", sizeof(MinSched) + nSlots * sizeof(MinTmr), 0);
  sched->interval = interval;
  sched->cb = cb;
  sched->ctx = ctx;
  sched->nSlots = nSlots;
  sched->slotMask = nSlots - 1;
  sched->lastSlot = nSlots - 1;
  sched->nextTime = rte_get_tsc_cycles();

  N_LOGI("New sched=%p slots=%" PRIu16 " interval=%" PRIu64 " cb=%p", sched, sched->nSlots,
         sched->interval, cb);

  for (uint32_t i = 0; i < nSlots; ++i) {
    MinTmr* slot = &sched->slot[i];
    slot->next = slot->prev = slot;
  }
  return sched;
}

void
MinSched_Close(MinSched* sched)
{
  rte_free(sched);
}

void
MinSched_Trigger_(MinSched* sched, TscTime now)
{
  while (sched->nextTime <= now) {
    sched->lastSlot = (sched->lastSlot + 1) & sched->slotMask;
    MinTmr* slot = &sched->slot[sched->lastSlot];
    N_LOGV("Trigger sched=%p slot=%" PRIu16 " time=%" PRIu64 " now=%" PRIu64, sched,
           sched->lastSlot, sched->nextTime, now);
    sched->nextTime += sched->interval;

    MinTmr* next;
    for (MinTmr* tmr = slot->next; tmr != slot; tmr = next) {
      next = tmr->next;
      MinTmr_Init(tmr);
      // clear timer before invoking callback, because callback could reschedule timer
      N_LOGD("Trigger sched=%p slot=%" PRIu16 " tmr=%p", sched, sched->lastSlot, tmr);
      ++sched->nTriggered;
      (sched->cb)(tmr, sched->ctx);
    }
    slot->next = slot->prev = slot;
  }
}

__attribute__((nonnull)) static __rte_always_inline void
MinTmr_Cancel2(MinTmr* tmr)
{
  tmr->next->prev = tmr->prev;
  tmr->prev->next = tmr->next;
}

void
MinTmr_Cancel_(MinTmr* tmr)
{
  N_LOGD("Cancel tmr=%p", tmr);
  MinTmr_Cancel2(tmr);
  MinTmr_Init(tmr);
}

bool
MinTmr_After(MinTmr* tmr, TscDuration after, MinSched* sched)
{
  if (tmr->next != NULL) {
    MinTmr_Cancel2(tmr);
  }

  uint64_t nSlotsAway = RTE_MAX(after, 0) / sched->interval + 1;
  if (unlikely(nSlotsAway >= sched->nSlots)) {
    N_LOGW("After(too-far) sched=%p tmr=%p after=%" PRId64 " nSlotsAway=%" PRIu64, sched, tmr,
           after, nSlotsAway);
    MinTmr_Init(tmr);
    return false;
  }

  uint32_t slotNum = (sched->lastSlot + nSlotsAway) & sched->slotMask;
  N_LOGD("After sched=%p tmr=%p after=%" PRId64 " slot=%" PRIu16 " last=%" PRIu16, sched, tmr,
         after, slotNum, sched->lastSlot);
  MinTmr* slot = &sched->slot[slotNum];
  tmr->next = slot->next;
  tmr->next->prev = tmr;
  slot->next = tmr;
  tmr->prev = slot;
  return true;
}
