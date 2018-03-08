#include "pcct.h"

#include "cs.h"
#include "pit.h"

#include "../../core/logger.h"

INIT_ZF_LOG(Pcct);

#undef uthash_malloc
#undef uthash_free
#undef uthash_memcmp
#define uthash_malloc(sz) rte_malloc("PCCT.uthash", (sz), 0)
#define uthash_free(ptr, sz) rte_free((ptr))
#define uthash_memcmp(a, b, n)                                                 \
  (!PccKey_MatchSearchKey((const PccKey*)(a), (const PccSearch*)(b)))

#define PCCT_TOKEN_MASK (((uint64_t)1 << 48) - 1)

static uint32_t
__Pcct_TokenHt_Hash(const void* key, uint32_t keyLen, uint32_t initVal)
{
  assert(false); // rte_hash_function should not be invoked
  return 0;
}

static int
__Pcct_TokenHt_Cmp(const void* key1, const void* key2, size_t kenLen)
{
  assert(kenLen == sizeof(uint64_t));
  return *(const uint64_t*)key1 != *(const uint64_t*)key2;
}

Pcct*
Pcct_New(const char* id, uint32_t maxEntries, unsigned numaSocket)
{
  char tokenHtName[RTE_HASH_NAMESIZE];
  int tokenHtNameLen =
    snprintf(tokenHtName, sizeof(tokenHtName), "%s.token", id);
  if (tokenHtNameLen < 0 || tokenHtNameLen >= sizeof(tokenHtName)) {
    rte_errno = ENAMETOOLONG;
    return NULL;
  }

  Pcct* pcct = (Pcct*)rte_mempool_create(
    id, maxEntries, sizeof(PccEntry), 0, sizeof(PcctPriv), NULL, NULL, NULL,
    NULL, numaSocket, MEMPOOL_F_SP_PUT | MEMPOOL_F_SC_GET);
  if (unlikely(pcct == NULL)) {
    return NULL;
  }

  PcctPriv* pcctp = Pcct_GetPriv(pcct);
  memset(pcctp, 0, sizeof(*pcctp));

  struct rte_hash_parameters tokenHtParams = {
    .name = tokenHtName,
    .entries = maxEntries,
    .key_len = sizeof(uint64_t), // waste 2 bytes for faster comparison
    .hash_func = __Pcct_TokenHt_Hash,
    .socket_id = numaSocket,
  };
  pcctp->tokenHt = rte_hash_create(&tokenHtParams);
  rte_hash_set_cmp_func(pcctp->tokenHt, __Pcct_TokenHt_Cmp);

  ZF_LOGI("%p New('%s')", pcct, id);
  return pcct;
}

void
Pcct_Close(Pcct* pcct)
{
  ZF_LOGI("%p Close()", pcct);

  PcctPriv* pcctp = Pcct_GetPriv(pcct);
  rte_hash_free(pcctp->tokenHt);
  HASH_CLEAR(hh, pcctp->keyHt);
  rte_mempool_free(Pcct_ToMempool(pcct));
}

PccEntry*
Pcct_Insert(Pcct* pcct, uint64_t hash, PccSearch* search, bool* isNew)
{
  PccEntry* entry = Pcct_Find(pcct, hash, search);
  if (entry != NULL) {
    *isNew = false;
    return entry;
  }

  void* entry0;
  int res = rte_mempool_get(Pcct_ToMempool(pcct), &entry0);
  if (unlikely(res != 0)) {
    ZF_LOGE("%p Insert() table-full", pcct);
    return NULL;
  }

  entry = (PccEntry*)entry0;
  PccKey_CopyFromSearch(&entry->key, search);
  entry->__tokenQword = 0;

  PcctPriv* pcctp = Pcct_GetPriv(pcct);
  HASH_ADD_BYHASHVALUE(hh, pcctp->keyHt, key, 0, hash, entry);
  *isNew = true;

  ZF_LOGD("%p Insert(%016" PRIx64 ", %s) %p", pcct, hash,
          PccSearch_ToDebugString(search), entry);
  return entry;
}

void
Pcct_Erase(Pcct* pcct, PccEntry* entry)
{
  ZF_LOGD("%p Erase(%p)", pcct, entry);

  PcctPriv* pcctp = Pcct_GetPriv(pcct);
  Pcct_RemoveToken(pcct, entry);
  HASH_DELETE(hh, pcctp->keyHt, entry);
  rte_mempool_put(Pcct_ToMempool(pcct), entry);
}

PccEntry*
Pcct_Find(const Pcct* pcct, uint64_t hash, PccSearch* search)
{
  PcctPriv* pcctp = Pcct_GetPriv(pcct);
  PccEntry* entry = NULL;
  HASH_FIND_BYHASHVALUE(hh, pcctp->keyHt, search, 0, hash, entry);
  return entry;
}

uint64_t
__Pcct_AddToken(Pcct* pcct, PccEntry* entry)
{
  assert(!entry->hasToken);

  PcctPriv* pcctp = Pcct_GetPriv(pcct);

  // find an available token; it must exist because 48-bit token space is larger than maxEntries
  while (Pcct_FindByToken(pcct, ++pcctp->lastToken) != NULL) {
  }
  uint64_t token = pcctp->lastToken & PCCT_TOKEN_MASK;
  uint32_t hash = (uint32_t)token;

  entry->token = token;
  entry->hasToken = true;
  int res =
    rte_hash_add_key_with_hash_data(pcctp->tokenHt, &token, hash, entry);
  assert(res == 0);

  ZF_LOGD("%p AddToken(%p) %012" PRIx64, pcct, entry, token);
  return token;
}

void
__Pcct_RemoveToken(Pcct* pcct, PccEntry* entry)
{
  assert(entry->hasToken);
  assert(Pcct_FindByToken(pcct, entry->token) == entry);

  PcctPriv* pcctp = Pcct_GetPriv(pcct);

  uint64_t token = entry->token;
  uint32_t hash = (uint32_t)token;

  ZF_LOGD("%p RemoveToken(%p, %012" PRIx64 ")", pcct, entry, token);

  entry->hasToken = false;
  int res = rte_hash_del_key_with_hash(pcctp->tokenHt, &token, hash);
  assert(res >= 0);
}

PccEntry*
Pcct_FindByToken(const Pcct* pcct, uint64_t token)
{
  PcctPriv* pcctp = Pcct_GetPriv(pcct);

  token &= PCCT_TOKEN_MASK;
  uint32_t hash = (uint32_t)token;

  void* entry = NULL;
  int res =
    rte_hash_lookup_with_hash_data(pcctp->tokenHt, &token, hash, &entry);
  // DPDK Doxygen says rte_hash_lookup_with_hash_data returns 0 if found, ENOENT if not found;
  // but in DPDK 17.11 code it returns entry position if found, -ENOENT if not found.
  assert((res >= 0 && entry != NULL) || (res == -ENOENT && entry == NULL));
  return (PccEntry*)entry;
}
