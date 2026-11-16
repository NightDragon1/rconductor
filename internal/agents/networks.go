// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agents

import (
	"log/slog"
	"net"
	"net/netip"
)

// panelNetworks are the networks of the panel's own interfaces, loopback
// included. An agent connecting from one of them shares a Docker network
// (or the host) with the panel, so its address names no host (D-084).
func panelNetworks() ([]netip.Prefix, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(addrs))
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out, nil
}

// outsidePanel tells whether the address an agent connected from lies
// outside the panel's networks and is one other hosts reach (D-084). When
// the networks cannot be read, it is not shown.
func (s *Service) outsidePanel(remote string) bool {
	ip, err := netip.ParseAddr(remote)
	if err != nil {
		return false
	}
	ip = ip.Unmap().WithZone("")
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	nets, err := s.networks()
	if err != nil {
		s.log.Warn("panel networks not read", slog.Any("err", err))
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
