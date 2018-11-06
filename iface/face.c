#include "face.h"
#include "rx-proc.h"

static const int LATENCY_STAT_SAMPLE_FREQ = 16;
static const int TX_BURST_FRAMES = 64;  // number of frames in a burst
static const int TX_MAX_FRAGMENTS = 64; // max allowed number of fragments

Face __gFaces[FACEID_MAX + 1];

static void
Face_TxBurst_SendFrames(Face* face, struct rte_mbuf** frames, uint16_t nFrames)
{
  assert(nFrames > 0);
  uint16_t nQueued = (*face->txBurstOp)(face, frames, nFrames);
  uint16_t nRejects = nFrames - nQueued;
  FreeMbufs(&frames[nQueued], nRejects);
  TxProc_CountQueued(&face->impl->tx, nQueued, nRejects);
}

void
Face_TxBurst_Nts(Face* face, Packet** npkts, uint16_t count)
{
  struct rte_mbuf* frames[TX_BURST_FRAMES + TX_MAX_FRAGMENTS];
  uint16_t nFrames = 0;

  TscTime now = rte_get_tsc_cycles();
  for (uint16_t i = 0; i < count; ++i) {
    Packet* npkt = npkts[i];
    TscDuration timeSinceRx = now - Packet_ToMbuf(npkt)->timestamp;
    RunningStat_Push1(&face->impl->latencyStat, timeSinceRx);

    struct rte_mbuf** outFrames = &frames[nFrames];
    nFrames +=
      TxProc_Output(&face->impl->tx, npkt, outFrames, TX_MAX_FRAGMENTS);

    if (unlikely(nFrames >= TX_BURST_FRAMES)) {
      Face_TxBurst_SendFrames(face, frames, nFrames);
      nFrames = 0;
    }
  }

  if (likely(nFrames > 0)) {
    Face_TxBurst_SendFrames(face, frames, nFrames);
  }
}

void
FaceImpl_Init(Face* face, uint16_t mtu, uint16_t headroom,
              FaceMempools* mempools)
{
  face->threadSafeTxQueue = NULL;

  RunningStat_SetSampleRate(&face->impl->latencyStat, LATENCY_STAT_SAMPLE_FREQ);
  TxProc_Init(&face->impl->tx, mtu, headroom, mempools->indirectMp,
              mempools->headerMp);
  RxProc_Init(&face->impl->rx, mempools->nameMp);
}

void
FaceImpl_RxBurst(FaceRxBurst* burst, uint16_t nFrames, int rxThread,
                 Face_RxCb cb, void* cbarg)
{
  FaceRxBurst_Clear(burst);

  struct rte_mbuf** frames = FaceRxBurst_GetScratch(burst);
  for (uint16_t i = 0; i < nFrames; ++i) {
    struct rte_mbuf* frame = frames[i];
    Face* face = __Face_Get(frame->port);
    if (unlikely(face->impl == NULL)) {
      rte_pktmbuf_free(frame);
      continue;
    }

    Packet* npkt = RxProc_Input(&face->impl->rx, rxThread, frame);
    if (npkt == NULL) {
      continue;
    }

    L3PktType l3type = Packet_GetL3PktType(npkt);
    switch (l3type) {
      case L3PktType_Interest:
        FaceRxBurst_PutInterest(burst, npkt);
        break;
      case L3PktType_Data:
        FaceRxBurst_PutData(burst, npkt);
        break;
      case L3PktType_Nack:
        FaceRxBurst_PutNack(burst, npkt);
        break;
    }
  }

  if (likely(burst->nInterests + burst->nData + burst->nNacks > 0)) {
    cb(burst, cbarg);
  }
}
