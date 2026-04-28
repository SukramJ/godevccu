// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
)

// newRPCWithLink returns an RPCFunctions loaded with HM-Sen-MDIR-WM55,
// which has channels carrying a LINK paramset description.
//
// The device fixture address is VCU0000274; channels :1, :2, :3 each
// have LINK descriptions (PEER_NEEDS_BURST, EXPECT_AES).
func newRPCWithLink(t *testing.T) *ccu.RPCFunctions {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HM-Sen-MDIR-WM55"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	return rpc
}

// ─────────────────────────────────────────────────────────────────────────────
// getParamset(addr, "LINK") — literal LINK key returns description defaults
// ─────────────────────────────────────────────────────────────────────────────

// TestGetParamsetLinkKeyReturnsDefaults verifies that calling
// getParamset(channelAddr, "LINK") returns the default values built from the
// channel's LINK paramset description. No link entry needs to exist.
func TestGetParamsetLinkKeyReturnsDefaults(t *testing.T) {
	rpc := newRPCWithLink(t)

	// VCU0000274:1 has LINK description with PEER_NEEDS_BURST and EXPECT_AES.
	vals, err := rpc.GetParamset("VCU0000274:1", hmconst.ParamsetAttrLink)
	if err != nil {
		t.Fatalf("GetParamset(LINK): %v", err)
	}
	// Both parameters have DEFAULT=false.
	if _, ok := vals["PEER_NEEDS_BURST"]; !ok {
		t.Error("PEER_NEEDS_BURST missing from LINK defaults")
	}
	if _, ok := vals["EXPECT_AES"]; !ok {
		t.Error("EXPECT_AES missing from LINK defaults")
	}
}

// TestGetParamsetLinkKeyNoDescReturnsEmpty verifies that a channel without a
// LINK description returns an empty map (not an error) when the literal "LINK"
// key is used.
func TestGetParamsetLinkKeyNoDescReturnsEmpty(t *testing.T) {
	rpc := newRPC(t) // HmIP-SWSD — no LINK descriptions on its channels

	// HmIP-SWSD:0 has no LINK description.
	vals, err := rpc.GetParamset("VCU2822385:0", hmconst.ParamsetAttrLink)
	if err != nil {
		t.Fatalf("GetParamset(LINK) on channel without LINK desc: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty map for channel without LINK desc, got %v", vals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// getParamset(addr, peerAddr) — peer-address form returns empty when no link
// ─────────────────────────────────────────────────────────────────────────────

// TestGetParamsetLinkReturnsEmptyForNoLinks verifies that the peer-address form
// returns an empty map when no link has been registered for the pair.
func TestGetParamsetLinkReturnsEmptyForNoLinks(t *testing.T) {
	rpc := newRPCWithLink(t)

	vals, err := rpc.GetParamset("VCU0000274:1", "VCU0000274:2")
	if err != nil {
		t.Fatalf("GetParamset(addr, peerAddr) with no link: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty map for non-existent link, got %v", vals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// addLink → getParamset(addr, peerAddr) populates defaults
// ─────────────────────────────────────────────────────────────────────────────

// TestAddLinkPopulatesLinkParamset verifies that AddLink allocates a default
// LINK paramset that can be retrieved via GetLinkParamset.
func TestAddLinkPopulatesLinkParamset(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	receiver := "VCU0000274:2"
	rpc.AddLink(sender, receiver, "test-link", "test desc")

	// Retrieve via the peer-address form.
	vals, err := rpc.GetLinkParamset(sender, receiver)
	if err != nil {
		t.Fatalf("GetLinkParamset after AddLink: %v", err)
	}
	// HM-Sen-MDIR-WM55 channel :1 has LINK desc → defaults must be present.
	if len(vals) == 0 {
		t.Fatal("expected non-empty paramset after AddLink; sender channel has LINK description")
	}
	if _, ok := vals["PEER_NEEDS_BURST"]; !ok {
		t.Error("PEER_NEEDS_BURST missing from LINK paramset after AddLink")
	}
}

// TestAddLinkIsIdempotent verifies that calling AddLink for the same pair twice
// does not return an error and does not overwrite existing paramset values.
func TestAddLinkIsIdempotent(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	receiver := "VCU0000274:2"

	rpc.AddLink(sender, receiver, "first", "")
	// Write a value via PutLinkParamset so we can detect an overwrite.
	if err := rpc.PutLinkParamset(sender, receiver, map[string]any{"PEER_NEEDS_BURST": true}); err != nil {
		t.Fatalf("PutLinkParamset: %v", err)
	}

	// Second AddLink must not reset existing values.
	rpc.AddLink(sender, receiver, "second", "")

	vals, err := rpc.GetLinkParamset(sender, receiver)
	if err != nil {
		t.Fatalf("GetLinkParamset: %v", err)
	}
	if v, ok := vals["PEER_NEEDS_BURST"]; !ok || v != true {
		t.Fatalf("AddLink idempotency: PEER_NEEDS_BURST value was reset, got %v", vals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// putParamset(addr, peerAddr, {…}) persists values
// ─────────────────────────────────────────────────────────────────────────────

// TestPutParamsetLinkPersistsValues verifies that putParamset with a peer
// address key stores the supplied values and getParamset reads them back.
func TestPutParamsetLinkPersistsValues(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	peer := "SOME-REMOTE:3"

	// Write via PutParamset (the peer-address calling form).
	err := rpc.PutParamset(sender, peer, map[string]any{"PEER_NEEDS_BURST": true}, false)
	if err != nil {
		t.Fatalf("PutParamset(peerAddr): %v", err)
	}

	// Read back via GetParamset.
	vals, err := rpc.GetParamset(sender, peer)
	if err != nil {
		t.Fatalf("GetParamset(peerAddr) after PutParamset: %v", err)
	}
	if v, ok := vals["PEER_NEEDS_BURST"]; !ok || v != true {
		t.Fatalf("PEER_NEEDS_BURST = %v, want true", vals["PEER_NEEDS_BURST"])
	}
}

// TestPutLinkParamsetMergesValues verifies that multiple PutLinkParamset calls
// merge values rather than replacing the whole map.
func TestPutLinkParamsetMergesValues(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	peer := "VCU0000274:2"
	rpc.AddLink(sender, peer, "merge-test", "")

	if err := rpc.PutLinkParamset(sender, peer, map[string]any{"PEER_NEEDS_BURST": true}); err != nil {
		t.Fatalf("PutLinkParamset #1: %v", err)
	}
	if err := rpc.PutLinkParamset(sender, peer, map[string]any{"EXPECT_AES": true}); err != nil {
		t.Fatalf("PutLinkParamset #2: %v", err)
	}

	vals, err := rpc.GetLinkParamset(sender, peer)
	if err != nil {
		t.Fatalf("GetLinkParamset: %v", err)
	}
	if v, ok := vals["PEER_NEEDS_BURST"]; !ok || v != true {
		t.Fatalf("PEER_NEEDS_BURST after merge: %v", v)
	}
	if v, ok := vals["EXPECT_AES"]; !ok || v != true {
		t.Fatalf("EXPECT_AES after merge: %v", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// removeLink clears the entry
// ─────────────────────────────────────────────────────────────────────────────

// TestRemoveLinkClearsLinkParamset verifies that RemoveLink drops the stored
// LINK paramset for the pair; subsequent reads return an empty map.
func TestRemoveLinkClearsLinkParamset(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	peer := "VCU0000274:2"
	rpc.AddLink(sender, peer, "clear-test", "")

	// Ensure the entry exists.
	vals, _ := rpc.GetLinkParamset(sender, peer)
	if len(vals) == 0 {
		t.Fatal("expected non-empty paramset after AddLink")
	}

	rpc.RemoveLink(sender, peer)

	vals, err := rpc.GetLinkParamset(sender, peer)
	if err != nil {
		t.Fatalf("GetLinkParamset after RemoveLink: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty map after RemoveLink, got %v", vals)
	}
}

// TestRemoveLinkUnknownPairIsNoError verifies that RemoveLink on an unknown
// (sender, receiver) pair is a no-error no-op.
func TestRemoveLinkUnknownPairIsNoError(t *testing.T) {
	rpc := newRPCWithLink(t)
	// Must not panic.
	rpc.RemoveLink("NONEXISTENT:1", "NONEXISTENT:2")
	rpc.RemoveLink("NONEXISTENT:1", "NONEXISTENT:2") // idempotent
}

// ─────────────────────────────────────────────────────────────────────────────
// getParamsetDescription(addr, "LINK")
// ─────────────────────────────────────────────────────────────────────────────

// TestGetParamsetDescriptionLinkReturnsTemplate verifies that
// GetParamsetDescription with the literal "LINK" key returns the paramset
// descriptor built from the embedded JSON.
func TestGetParamsetDescriptionLinkReturnsTemplate(t *testing.T) {
	rpc := newRPCWithLink(t)

	desc, err := rpc.GetParamsetDescription("VCU0000274:1", hmconst.ParamsetAttrLink)
	if err != nil {
		t.Fatalf("GetParamsetDescription(LINK): %v", err)
	}
	if len(desc) == 0 {
		t.Fatal("expected non-empty LINK paramset description")
	}
	if _, ok := desc["PEER_NEEDS_BURST"]; !ok {
		t.Error("PEER_NEEDS_BURST missing from LINK description")
	}
	if _, ok := desc["EXPECT_AES"]; !ok {
		t.Error("EXPECT_AES missing from LINK description")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// getLinks / getLinkPeers return actual data
// ─────────────────────────────────────────────────────────────────────────────

// TestGetLinksReturnsActualLinks verifies that GetLinks returns one entry per
// registered link for the given sender address.
func TestGetLinksReturnsActualLinks(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	peer1 := "VCU0000274:2"
	peer2 := "VCU0000274:3"
	rpc.AddLink(sender, peer1, "link-a", "")
	rpc.AddLink(sender, peer2, "link-b", "")

	links := rpc.GetLinks(sender, 0)
	if len(links) != 2 {
		t.Fatalf("GetLinks: got %d links, want 2", len(links))
	}
	receivers := make(map[string]bool)
	for _, l := range links {
		m, ok := l.(map[string]any)
		if !ok {
			t.Fatalf("link entry is not a map: %T", l)
		}
		recv, _ := m["RECEIVER"].(string)
		receivers[recv] = true
		if _, hasSender := m["SENDER"]; !hasSender {
			t.Error("link entry missing SENDER key")
		}
	}
	if !receivers[strings.ToUpper(peer1)] {
		t.Errorf("peer %q missing from GetLinks result", peer1)
	}
	if !receivers[strings.ToUpper(peer2)] {
		t.Errorf("peer %q missing from GetLinks result", peer2)
	}
}

// TestGetLinksEmptyAddressReturnsAll verifies that GetLinks("", 0) returns all
// registered links across all senders.
func TestGetLinksEmptyAddressReturnsAll(t *testing.T) {
	rpc := newRPCWithLink(t)

	rpc.AddLink("VCU0000274:1", "VCU0000274:2", "a", "")
	rpc.AddLink("VCU0000274:2", "VCU0000274:3", "b", "")

	all := rpc.GetLinks("", 0)
	if len(all) != 2 {
		t.Fatalf("GetLinks(\"\"): got %d links, want 2", len(all))
	}
}

// TestGetLinkPeersReturnsActualPeers verifies that GetLinkPeers returns only
// the receiver addresses for links originating from the given sender.
func TestGetLinkPeersReturnsActualPeers(t *testing.T) {
	rpc := newRPCWithLink(t)

	sender := "VCU0000274:1"
	peer1 := "VCU0000274:2"
	peer2 := "VCU0000274:3"
	rpc.AddLink(sender, peer1, "p1", "")
	rpc.AddLink(sender, peer2, "p2", "")

	peers := rpc.GetLinkPeers(sender)
	if len(peers) != 2 {
		t.Fatalf("GetLinkPeers: got %d peers, want 2: %v", len(peers), peers)
	}
	seen := make(map[string]bool)
	for _, p := range peers {
		seen[p] = true
	}
	if !seen[strings.ToUpper(peer1)] {
		t.Errorf("peer %q missing from GetLinkPeers result", peer1)
	}
	if !seen[strings.ToUpper(peer2)] {
		t.Errorf("peer %q missing from GetLinkPeers result", peer2)
	}
}

// TestGetLinkPeersUnknownChannelReturnsEmpty verifies that GetLinkPeers for an
// address with no links returns an empty slice.
func TestGetLinkPeersUnknownChannelReturnsEmpty(t *testing.T) {
	rpc := newRPCWithLink(t)
	peers := rpc.GetLinkPeers("VCU0000274:1")
	if len(peers) != 0 {
		t.Fatalf("expected empty peers, got %v", peers)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Case-insensitive address handling
// ─────────────────────────────────────────────────────────────────────────────

// TestLinkParamsetCaseInsensitive verifies that address comparisons are
// case-insensitive for all link operations.
func TestLinkParamsetCaseInsensitive(t *testing.T) {
	rpc := newRPCWithLink(t)

	rpc.AddLink("vcu0000274:1", "vcu0000274:2", "ci-test", "")

	vals, err := rpc.GetLinkParamset("VCU0000274:1", "VCU0000274:2")
	if err != nil {
		t.Fatalf("GetLinkParamset (upper after lower AddLink): %v", err)
	}
	// The sender channel has a LINK description, so defaults must be non-empty.
	if len(vals) == 0 {
		t.Fatal("expected non-empty paramset (case-insensitive lookup)")
	}

	// Remove using mixed case.
	rpc.RemoveLink("Vcu0000274:1", "Vcu0000274:2")
	vals2, _ := rpc.GetLinkParamset("VCU0000274:1", "VCU0000274:2")
	if len(vals2) != 0 {
		t.Fatalf("expected empty after RemoveLink (mixed case), got %v", vals2)
	}
}
