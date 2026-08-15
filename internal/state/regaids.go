// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state

import "strings"

// ReGa object ids.
//
// A CCU keeps every object — device, channel, program, variable, room,
// function — in one ReGa object model, and each carries a numeric id
// that clients cache and cross-reference: Room.getAll reports the
// *channel ids* it contains, and Device.listAllDetail reports the same
// ids on its channels. Without ids for devices and channels those
// cross-references point at nothing, which is why room and function
// assignments read as empty on the simulator.
//
// Programs (1000+), variables (2000+), rooms (3000+), functions (4000+)
// and service messages (5000+) already had their own ranges; devices and
// channels get 6000+ here. The exact numbers are not observable as
// anything but opaque handles — what matters is that they are stable
// within a run and unique across object kinds.
const regaDeviceIDBase = 6000

// RegisterAddress assigns (or returns) the ReGa id of a device or
// channel address. Addresses are matched case-insensitively and stored
// upper-case, following the project-wide address rule.
func (m *Manager) RegisterAddress(address string) int {
	if address == "" {
		return 0
	}
	key := strings.ToUpper(address)
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.regaIDByAddress[key]; ok {
		return id
	}
	id := m.nextRegaID
	m.nextRegaID++
	m.regaIDByAddress[key] = id
	m.regaAddressByID[id] = key
	return id
}

// RegaID returns the id assigned to an address, or 0 when the address
// was never registered.
func (m *Manager) RegaID(address string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.regaIDByAddress[strings.ToUpper(address)]
}

// RegaAddress is the reverse lookup: the address behind an id, or the
// empty string.
func (m *Manager) RegaAddress(id int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.regaAddressByID[id]
}

// RegisterAddresses assigns ids to a batch of addresses in the given
// order, so ids follow the catalogue order rather than map iteration.
func (m *Manager) RegisterAddresses(addresses []string) {
	for _, addr := range addresses {
		m.RegisterAddress(addr)
	}
}

// ChannelIDsForAddresses maps channel addresses to their ReGa ids,
// skipping unregistered ones. Rooms and functions store addresses;
// clients expect ids.
func (m *Manager) ChannelIDsForAddresses(addresses []string) []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]int, 0, len(addresses))
	for _, addr := range addresses {
		if id, ok := m.regaIDByAddress[strings.ToUpper(addr)]; ok {
			out = append(out, id)
		}
	}
	return out
}
