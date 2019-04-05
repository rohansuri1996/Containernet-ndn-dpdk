#ifndef NDN_DPDK_APP_NDNPING_SERVER_H
#define NDN_DPDK_APP_NDNPING_SERVER_H

/// \file

#include "../../container/nameset/nameset.h"
#include "../../dpdk/thread.h"
#include "../../iface/face.h"

#define NDNPINGSERVER_BURST_SIZE 64
#define NDNPINGSERVER_PAYLOAD_MAX 65536

/** \brief Per-pattern information in ndnping server.
 */
typedef struct NdnpingServerPattern
{
  LName nameSuffix;
  uint16_t payloadL;

  uint64_t nInterests;

  char nameSuffixV[0];
} NdnpingServerPattern;

/** \brief ndnping server.
 */
typedef struct NdnpingServer
{
  struct rte_ring* rxQueue;
  struct rte_mempool* dataMp; ///< mempool for Data
  uint16_t dataMbufHeadroom;
  FaceId face;

  uint32_t freshnessPeriod;
  NameSet patterns;     ///< served prefixes
  bool wantNackNoRoute; ///< whether to Nack unserved Interests

  ThreadStopFlag stop;

  uint64_t nNoMatch;
  uint64_t nAllocError;
} NdnpingServer;

void
NdnpingServer_Run(NdnpingServer* server);

#endif // NDN_DPDK_APP_NDNPING_SERVER_H
