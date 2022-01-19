package ethnetif

import (
	"errors"
	"fmt"

	"github.com/peterbourgon/mergemap"
	"github.com/usnistgov/ndn-dpdk/core/pciaddr"
	"github.com/usnistgov/ndn-dpdk/dpdk/ethdev"
)

// XDPProgram is the absolute path to an XDP program ELF object.
// This should be assigned by package main.
var XDPProgram string

// DriverKind indicates the kind of driver requested for a network interface.
type DriverKind string

const (
	DriverPCI      DriverKind = "PCI"
	DriverXDP      DriverKind = "XDP"
	DriverAfPacket DriverKind = "AF_PACKET"
)

// Config selects a network interface and creates an EthDev.
type Config struct {
	Driver  DriverKind             `json:"driver" gqldesc:"EthDev driver kind."`
	Netif   string                 `json:"netif,omitempty" gqldesc:"Network interface name (XDP, AF_PACKET, bifurcated PCI devices)."`
	PCIAddr *pciaddr.PCIAddress    `json:"pciAddr,omitempty" gqldesc:"PCI address (PCI devices)."`
	DevArgs map[string]interface{} `json:"devargs,omitempty" gqldesc:"DPDK device arguments."`

	SkipEthtool bool `json:"skipEthtool,omitempty" gqldesc:"Don't perform ethtool updates for XDP."`
}

// CreateEthDev creates an Ethernet device.
func CreateEthDev(cfg Config) (ethdev.EthDev, error) {
	if cfg.Netif != "" {
		if n, e := netIntfByName(cfg.Netif); e == nil {
			if dev := n.FindDev(); dev != nil {
				return dev, nil
			}
		}
	}

	switch cfg.Driver {
	case DriverPCI:
		return createPCI(cfg)
	case DriverXDP:
		return createXDP(cfg)
	case DriverAfPacket:
		return createAfPacket(cfg)
	}
	return nil, errors.New("invalid DriverKind")
}

func createPCI(cfg Config) (ethdev.EthDev, error) {
	var addr pciaddr.PCIAddress
	switch {
	case cfg.Netif != "":
		n, e := netIntfByName(cfg.Netif)
		if e != nil {
			return nil, e
		}
		if addr, e = n.PCIAddr(); e != nil {
			return nil, fmt.Errorf("cannot determine PCI address for %s: %w", cfg.Netif, e)
		}
	case cfg.PCIAddr != nil:
		addr = *cfg.PCIAddr
	default:
		return nil, errors.New("either netif or pciAddr must be specified")
	}

	if dev := ethdev.FromPCI(addr); dev != nil {
		return dev, nil
	}

	return ethdev.ProbePCI(addr, cfg.DevArgs)
}

func createXDP(cfg Config) (ethdev.EthDev, error) {
	n, e := netIntfByName(cfg.Netif)
	if e != nil {
		return nil, e
	}

	args := map[string]interface{}{
		"iface":       n.Name,
		"start_queue": 0,
		"queue_count": 1,
	}
	if XDPProgram != "" {
		args["xdp_prog"] = XDPProgram
	}
	args = mergemap.Merge(args, cfg.DevArgs)

	if !cfg.SkipEthtool {
		n.SetOneChannel()
		n.DisableVLANOffload()
		if prog, ok := args["xdp_prog"]; ok && prog != nil {
			n.UnloadXDP()
		}
	}

	return ethdev.NewVDev(ethdev.DriverXDP+"_"+n.Name, args, n.NumaSocket())
}

func createAfPacket(cfg Config) (ethdev.EthDev, error) {
	n, e := netIntfByName(cfg.Netif)
	if e != nil {
		return nil, e
	}

	args := map[string]interface{}{
		"iface":  n.Name,
		"qpairs": 1,
	}
	args = mergemap.Merge(args, cfg.DevArgs)

	return ethdev.NewVDev(ethdev.DriverAfPacket+"_"+n.Name, args, n.NumaSocket())
}
