/**
 * @file
 * The triggered strategy multicasts the Interests, observes when there is a packetdrop and then it starts with the roundrobin
 */
#include "api.h"

// how often to send probe Interest, in number of packets
#define PROBE_INTERVAL 1024