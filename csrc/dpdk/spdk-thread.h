#ifndef NDNDPDK_DPDK_SPDK_THREAD_H
#define NDNDPDK_DPDK_SPDK_THREAD_H

/** @file */

#include "thread.h"
#include <spdk/thread.h>

typedef struct SpdkThread
{
  ThreadCtrl ctrl;
  struct spdk_thread* spdkTh;
} SpdkThread;

int
SpdkThread_Run(SpdkThread* th);

#endif // NDNDPDK_DPDK_SPDK_THREAD_H
