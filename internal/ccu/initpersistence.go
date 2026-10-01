// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"encoding/json"
	"os"
	"strings"
)

// Persisted callback registrations.
//
// init() registrations live only in memory, so a restarted simulator
// forgets every client that had registered — while a rebooting CCU
// re-establishes them and starts pushing events again on its own. A
// client that does not re-register after a restart therefore goes
// silent against the simulator and keeps working against real hardware.
//
// The registrations are stored next to the paramset database, under the
// same [Options.Persistence] switch plus an explicit opt-in, since
// reconnecting on startup is a behaviour a test must ask for.

// initRegistrationsSuffix is appended to the paramset database path.
const initRegistrationsSuffix = ".init.json"

// EnableInitPersistence makes init registrations survive a restart.
// Requires persistence to be configured; without it this is a no-op.
func (r *RPCFunctions) EnableInitPersistence() {
	r.mu.Lock()
	enabled := r.persistence
	r.persistInit = enabled
	path := r.persistencePath + initRegistrationsSuffix
	r.mu.Unlock()
	if !enabled {
		return
	}
	r.restoreRegistrations(path)
}

// registrationsPath is where the registrations are stored.
func (r *RPCFunctions) registrationsPath() string {
	return r.persistencePath + initRegistrationsSuffix
}

// SaveRegistrations writes the current registrations. The server calls
// it on shutdown; fixtures call it directly.
func (r *RPCFunctions) SaveRegistrations() error { return r.saveRegistrations() }

// storedRegistration is one persisted init registration.
type storedRegistration struct {
	InterfaceID string `json:"interface_id"`
	URL         string `json:"url"`
}

// saveRegistrations writes the current registrations as a list, so two
// clients sharing an interface id under init semantics both survive.
func (r *RPCFunctions) saveRegistrations() error {
	r.mu.Lock()
	if !r.persistInit {
		r.mu.Unlock()
		return nil
	}
	registrations := make([]storedRegistration, 0, len(r.remotes))
	for _, reg := range r.remotes {
		registrations = append(registrations, storedRegistration{InterfaceID: reg.interfaceID, URL: reg.client.URL()})
	}
	path := r.registrationsPath()
	r.mu.Unlock()

	data, err := json.Marshal(registrations)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// restoreRegistrations re-registers the clients recorded by an earlier
// run. A stored entry whose client is gone gets dropped again by the
// normal transport-error handling on the first event. Both the list
// written now and the (interfaceID → URL) map of earlier releases are
// read.
func (r *RPCFunctions) restoreRegistrations(path string) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is caller-configured
	if err != nil {
		return
	}
	var registrations []storedRegistration
	if err := json.Unmarshal(raw, &registrations); err != nil {
		var legacy map[string]string
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return
		}
		for interfaceID, url := range legacy {
			registrations = append(registrations, storedRegistration{InterfaceID: interfaceID, URL: url})
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, stored := range registrations {
		if stored.InterfaceID == "" || strings.TrimSpace(stored.URL) == "" {
			continue
		}
		key := stored.InterfaceID
		if r.initSemantics {
			key = stored.URL
		}
		if _, exists := r.remotes[key]; exists {
			continue
		}
		r.remotes[key] = registration{interfaceID: stored.InterfaceID, client: r.newRemote(stored.URL)}
	}
}
