package socketface

import (
	"fmt"
	"net"

	"ndn-dpdk/iface/faceuri"
)

// Create a SocketFace from FaceUri.
// Caller is responsible for closing unused mempools if face creation fails.
func NewFromUri(remote, local *faceuri.FaceUri, cfg Config) (face *SocketFace, e error) {
	var conn net.Conn
	if local != nil && local.Scheme != remote.Scheme {
		return nil, fmt.Errorf("local scheme %s differs from remote scheme %s", local.Scheme, remote.Scheme)
	} else if remote.Scheme == "udp4" {
		raddr, e := net.ResolveUDPAddr(remote.Scheme, remote.Host)
		if e != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr(%s,%s): %v", remote.Scheme, remote.Host, e)
		}
		laddr := &net.UDPAddr{Port: raddr.Port}
		if local != nil {
			if laddr, e = net.ResolveUDPAddr(local.Scheme, local.Host); e != nil {
				return nil, fmt.Errorf("net.ResolveUDPAddr(%s,%s): %v", local.Scheme, local.Host, e)
			}
		}
		conn, e = net.DialUDP(remote.Scheme, laddr, raddr)
		if e != nil {
			return nil, fmt.Errorf("net.DialUDP(%s,%s,%s): %v", remote.Scheme, laddr, raddr, e)
		}
	} else if remote.Scheme != "tcp4" && remote.Scheme != "unix" {
		return nil, fmt.Errorf("unknown scheme %s", remote.Scheme)
	} else if local != nil {
		return nil, fmt.Errorf("%s scheme does not accept local FaceUri", remote.Scheme)
	} else {
		conn, e = net.Dial(remote.Scheme, remote.Host+remote.Path)
		if e != nil {
			return nil, fmt.Errorf("net.Dial(%s,%s): %v", remote.Scheme, remote.Host, e)
		}
	}

	return New(conn, cfg), nil
}