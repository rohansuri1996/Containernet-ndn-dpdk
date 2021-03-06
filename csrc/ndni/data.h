#ifndef NDNDPDK_NDNI_DATA_H
#define NDNDPDK_NDNI_DATA_H

/** @file */

#include "name.h"

/** @brief Parsed Data packet. */
typedef struct PData
{
  PName name;
  uint32_t freshness; ///< FreshnessPeriod in millis
  bool hasDigest;
  uint8_t digest[ImplicitDigestLength];
} PData;

/**
 * @brief Parse Data.
 * @param pkt a uniquely owned, possibly segmented, direct mbuf that contains Data TLV.
 * @return whether success.
 */
__attribute__((nonnull)) bool
PData_Parse(PData* data, struct rte_mbuf* pkt);

/** @brief Determine whether Data can satisfy Interest. */
__attribute__((nonnull)) DataSatisfyResult
PData_CanSatisfy(PData* data, PInterest* interest);

/**
 * @brief Prepare a crypto_op for Data digest computation.
 * @param npkt Data packet.
 * @param[out] op an allocated crypto_op; will be populated but not enqueued.
 */
__attribute__((nonnull)) void
DataDigest_Prepare(Packet* npkt, struct rte_crypto_op* op);

/**
 * @brief Enqueue crypto_ops for Data digest computation.
 * @return number of rejections, which have been freed.
 */
__attribute__((nonnull)) uint16_t
DataDigest_Enqueue(CryptoQueuePair cqp, struct rte_crypto_op** ops, uint16_t count);

/**
 * @brief Finish Data digest computation.
 * @param op a dequeued crypto_op; will be freed.
 * @return the Data packet, or NULL if crypto_op was unsuccessful.
 */
__attribute__((nonnull)) Packet*
DataDigest_Finish(struct rte_crypto_op* op);

/**
 * @brief Declare a buffer from preparing Data MetaInfo.
 * @param capacity maximum MetaInfo TLV-LENGTH.
 *
 * Specified @p capacity must have room for:
 * @li MetaInfo TLV-TYPE and TLV-LENGTH, 2 octets
 * @li ContentType TLV, 3 octets.
 * @li FreshnessPeriod TLV, 6 octets.
 * @li FinalBlockId TLV, 2 octets + maximum @c finalBlock.length .
 *
 * Additional requirements for @p capacity :
 * @li @c capacity+1 must be a multiple of 8, for alignment.
 * @li @c capacity-2 must be less than 0xFD, because MetaInfo TLV-LENGTH is assumed as 1-octet.
 */
#define DataEnc_MetaInfoBuffer(capacity)                                                           \
  struct                                                                                           \
  {                                                                                                \
    uint8_t size;                                                                                  \
    uint8_t value[capacity];                                                                       \
    static_assert(capacity - 2 < 0xFD && (capacity % 8 == 7 || capacity == 0), "");                \
  }

__attribute__((nonnull)) bool
DataEnc_PrepareMetaInfo_(void* metaBuf, size_t capacity, ContentType ct, uint32_t freshness,
                         LName finalBlock);

/**
 * @brief Prepare Data MetaInfo.
 * @param metaBuf pointer to DataEnc_MetaInfoBuffer.
 * @param ct ContentType numeric value.
 * @param freshness FreshnessPeriod numeric value.
 * @param finalBlock FinalBlockId TLV-VALUE.
 * @return whether success.
 * @post @c metaBuf->value contains MetaInfo TLV.
 */
#define DataEnc_PrepareMetaInfo(metaBuf, ct, freshness, finalBlock)                                \
  DataEnc_PrepareMetaInfo_((metaBuf), sizeof((metaBuf)->value), (ct), (freshness), (finalBlock))

#define DataEnc_MustPrepareMetaInfo(...)                                                           \
  do {                                                                                             \
    bool ok = DataEnc_PrepareMetaInfo(__VA_ARGS__);                                                \
    NDNDPDK_ASSERT(ok);                                                                            \
  } while (false)

/**
 * @brief Encode Data with payload.
 * @param prefix Data name prefix.
 * @param suffix Data name suffix.
 * @param metaBuf prepared DataEnc_MetaInfoBuffer.
 * @param m a uniquely owned, unsegmented, direct mbuf of Content payload.
 * @return encoded packet, same as @p m .
 * @retval NULL insufficient headroom or tailroom.
 */
__attribute__((nonnull)) Packet*
DataEnc_EncodePayload(LName prefix, LName suffix, const void* metaBuf, struct rte_mbuf* m);

/** @brief Data encoder optimized for traffic generator. */
typedef struct DataGen
{
  /**
   * @brief Template mbuf.
   *
   * This should contain name suffix TLV-VALUE and fields after Name.
   * Name TL and Data TL are not included.
   */
  struct rte_mbuf* tpl;

  /** @brief Size of name suffix TLV-VALUE at the beginning of @c tpl . */
  uint16_t suffixL;
} DataGen;

/**
 * @brief Encode Data with DataGen template.
 * @return encoded packet.
 * @retval NULL allocation failure.
 *
 * If @c align.linearize is false, encoded packet has a header mbuf that contains @p prefix and
 * and an indirect mbuf that clones the template. @c mp->header dataroom must be at least
 * @c RTE_PKTMBUF_DATAROOM+LpHeaderHeadroom+DataGenDataroom .
 *
 * If @c align.linearize is true, encoded packet has one or more copied mbufs. @c mp->packet
 * dataroom must be at least @c RTE_PKTMBUF_DATAROOM+LpHeaderHeadroom+align.fragmentPayloadSize .
 */
__attribute__((nonnull)) Packet*
DataGen_Encode(DataGen* gen, LName prefix, PacketMempools* mp, PacketTxAlign align);

#endif // NDNDPDK_NDNI_DATA_H
