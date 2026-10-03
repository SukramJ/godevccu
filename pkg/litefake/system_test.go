// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake_test

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/pkg/litefake"
)

const sysReadToken = "olt_44444444444444444444444444444444"

func startSystemFake(t *testing.T) *litefake.Fake {
	t.Helper()
	return startFake(t, litefake.Options{Tokens: map[string][]string{
		litefake.DefaultToken: {"*"},
		sysReadToken:          {"system:read"},
	}})
}

// TestSystemEnforcesRouteScopes pins the scopes of the system routes:
// system:read reads, power and backup are separate scopes, a missing
// one is 403 naming it.
func TestSystemEnforcesRouteScopes(t *testing.T) {
	f := startSystemFake(t)
	for _, path := range []string{
		"/api/system/v1/status", "/api/system/v1/time", "/api/system/v1/system-update",
		"/api/system/v1/service-messages", "/api/system/v1/groups", "/api/system/v1/radio/health",
	} {
		if resp, raw := get(t, f, path, sysReadToken); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d %s", path, resp.StatusCode, raw)
		}
	}
	cases := []struct{ method, path, scope string }{
		{http.MethodPost, "/api/system/v1/reboot", "power"},
		{http.MethodPost, "/api/system/v1/reboot/recovery", "power"},
		{http.MethodPost, "/api/system/v1/system-update/install", "power"},
		{http.MethodPost, "/api/system/v1/restore/check", "power"},
		{http.MethodGet, "/api/system/v1/backup", "backup"},
		{http.MethodPost, "/api/system/v1/groups", "system:write"},
	}
	for _, tc := range cases {
		resp, raw := send(t, f, tc.method, tc.path, sysReadToken, `{"confirm":true}`)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(raw), `"scope":"`+tc.scope+`"`) {
			t.Errorf("%s %s: %d %s, want 403 %s", tc.method, tc.path, resp.StatusCode, raw, tc.scope)
		}
	}
	if resp, _ := get(t, f, "/api/system/v1/status", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no credential: %d", resp.StatusCode)
	}
}

// TestSystemPowerActionsNeedConfirm pins the {"confirm":true} rule and
// the recorded request bodies.
func TestSystemPowerActionsNeedConfirm(t *testing.T) {
	f := startSystemFake(t)
	for _, path := range []string{"/api/system/v1/reboot", "/api/system/v1/halt", "/api/system/v1/reboot/recovery"} {
		if resp, raw := send(t, f, http.MethodPost, path, litefake.DefaultToken, `{}`); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s without confirm: %d %s", path, resp.StatusCode, raw)
		}
		resp, raw := send(t, f, http.MethodPost, path, litefake.DefaultToken, `{"confirm":true}`)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"ok":true`) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, raw)
		}
	}
	var confirmed int
	for _, c := range f.Calls() {
		if c.Path == "/api/system/v1/reboot/recovery" && string(c.Body) == `{"confirm":true}` && c.Status == http.StatusOK {
			confirmed++
		}
	}
	if confirmed != 1 {
		t.Errorf("recorded confirmed recovery reboots: %d", confirmed)
	}
}

// TestSystemBackupAndRestore pins the backup download and the restore
// check/apply round trip.
func TestSystemBackupAndRestore(t *testing.T) {
	f := startSystemFake(t)
	resp, raw := get(t, f, "/api/system/v1/backup", litefake.DefaultToken)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(raw, litefake.BackupBlob()) ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), litefake.BackupFileName) {
		t.Fatalf("backup: %d %q %q", resp.StatusCode, raw, resp.Header.Get("Content-Disposition"))
	}
	if resp, raw := send(t, f, http.MethodPost, "/api/system/v1/backup/run", litefake.DefaultToken, `{}`); resp.StatusCode != http.StatusAccepted {
		t.Errorf("backup run: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := send(t, f, http.MethodPost, "/api/system/v1/restore/check", litefake.DefaultToken, ""); resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, raw) != "corrupt" {
		t.Errorf("empty restore: %d %s", resp.StatusCode, raw)
	}
	resp, raw = send(t, f, http.MethodPost, "/api/system/v1/restore/check", litefake.DefaultToken, string(litefake.BackupBlob()),
		"Content-Type", "application/octet-stream")
	check := decode[struct {
		File  string `json:"file"`
		Check struct {
			OK bool `json:"ok"`
		} `json:"check"`
	}](t, string(raw))
	if resp.StatusCode != http.StatusOK || check.File == "" || !check.Check.OK {
		t.Fatalf("restore check: %d %s", resp.StatusCode, raw)
	}
	resp, raw = send(t, f, http.MethodPost, "/api/system/v1/restore/apply", litefake.DefaultToken, `{"file":"`+check.File+`"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"rebooting":true`) {
		t.Errorf("restore apply: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := send(t, f, http.MethodPost, "/api/system/v1/restore/apply", litefake.DefaultToken, `{"file":"nope"}`); resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, raw) != "restore-failed" {
		t.Errorf("restore unknown file: %d %s", resp.StatusCode, raw)
	}
}

// TestSystemUpdateStagesAndInstalls pins download → staged → install,
// and 409 when nothing is staged.
func TestSystemUpdateStagesAndInstalls(t *testing.T) {
	f := startSystemFake(t)
	if resp, _ := send(t, f, http.MethodPost, "/api/system/v1/system-update/install", litefake.DefaultToken, `{}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("install without staged: %d", resp.StatusCode)
	}
	f.SetUpdateAvailable(&litefake.AvailableUpdate{Version: "1.1.0", Newer: true})
	_, raw := get(t, f, "/api/system/v1/system-update", litefake.DefaultToken)
	if !strings.Contains(string(raw), `"version":"1.1.0"`) || !strings.Contains(string(raw), `"lite":`) {
		t.Errorf("update state: %s", raw)
	}
	if resp, raw := send(t, f, http.MethodPost, "/api/system/v1/system-update/download", litefake.DefaultToken, `{}`); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"version":"1.1.0"`) {
		t.Errorf("download: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := send(t, f, http.MethodPost, "/api/system/v1/system-update/install", litefake.DefaultToken, `{}`); resp.StatusCode != http.StatusOK {
		t.Errorf("install: %d", resp.StatusCode)
	}
}

// groupCandidates are two channels the HmIP heating-group type can take,
// in the shape a box lists them (channel address as id and as serial).
func groupCandidates() map[string][]litefake.GroupMember {
	return map[string][]litefake.GroupMember{"hmip.heating.group": {
		{ID: "0000000000AA01:1", Serial: "0000000000AA01:1", Type: "SENSOR_WINDOW"},
		{ID: "0000000000BB02:9", Serial: "0000000000BB02:9", Type: "SWITCH_ACTUATOR"},
	}}
}

// TestSystemGroupsLifecycle pins group create/read/update/delete against
// the answers an openccu-lite box gave (1.0.0-dev, read and written on
// 2026-10-03; names and addresses neutralised): members, candidates and
// former members are objects, a write answers the detail plus the members
// as devices to configure, and the detail carries the group device's name
// and every offered type.
func TestSystemGroupsLifecycle(t *testing.T) {
	f := startFake(t, litefake.Options{GroupCandidates: groupCandidates()})
	tok := litefake.DefaultToken
	const (
		window = `{"id":"0000000000AA01:1","serial":"0000000000AA01:1","type":"SENSOR_WINDOW"}`
		relay  = `{"id":"0000000000BB02:9","serial":"0000000000BB02:9","type":"SWITCH_ACTUATOR"}`
		types  = `"types":[{"id":"HomeMatic.heating","label":"Heating_Control"},{"id":"hmip.heating.group","label":"HmIP-Heizungssteuerung"}]`
	)
	has := func(step string, raw []byte, parts ...string) {
		t.Helper()
		for _, p := range parts {
			if !strings.Contains(string(raw), p) {
				t.Errorf("%s: answer lacks %s\n%s", step, p, raw)
			}
		}
	}

	resp, raw := send(t, f, http.MethodPost, "/api/system/v1/groups", tok, `{"name":"Heizung EG","type":"hmip.heating.group","members":["0000000000AA01:1"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	has("create", raw,
		`"device":"INT0000001"`, `"ref":"VirtualDevices.INT0000001"`, `"device_name":"Heizung EG INT0000001"`,
		`"members":[`+window+`]`, `"devices_to_configure":[`+window+`]`,
		`"assignable":[`+relay+`]`, `"leftover":[]`, types)
	if strings.Contains(string(raw), "type_label") {
		t.Errorf("create: a box answers no type_label on a group's detail\n%s", raw)
	}
	id := strconv.Itoa(decode[struct {
		ID int `json:"id"`
	}](t, string(raw)).ID)

	// A member of a group is leftover for the type, and for another group
	// of that type; the list names no device to configure.
	if _, raw := get(t, f, "/api/system/v1/groups/types", tok); true {
		has("types", raw, `"id":"hmip.heating.group","label":"HmIP-Heizungssteuerung","assignable":[`+relay+`],"leftover":[`+window+`]`,
			`"id":"HomeMatic.heating","label":"Heating_Control","assignable":[],"leftover":[]`)
	}
	if _, raw := get(t, f, "/api/system/v1/groups", tok); true {
		has("list", raw, `"type_label":"HmIP-Heizungssteuerung"`, `"devices_to_configure":[]`)
	}

	if resp, raw := send(t, f, http.MethodPost, "/api/system/v1/groups", tok, `{"name":"a\nb","type":"hmip.heating.group"}`); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(raw), `"field":"name"`) {
		t.Errorf("two-line name: %d %s", resp.StatusCode, raw)
	}

	resp, raw = send(t, f, http.MethodPut, "/api/system/v1/groups/"+id, tok, `{"members":["0000000000AA01:1","0000000000BB02:9"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: %d %s", resp.StatusCode, raw)
	}
	has("update", raw, `"members":[`+window+`,`+relay+`]`, `"devices_to_configure":[`+window+`,`+relay+`]`, `"assignable":[]`)

	// A name-only update keeps the members and renames the group device.
	_, raw = send(t, f, http.MethodPut, "/api/system/v1/groups/"+id, tok, `{"name":"Heizung OG"}`)
	has("rename", raw, `"name":"Heizung OG"`, `"device_name":"Heizung OG INT0000001"`, `"members":[`+window+`,`+relay+`]`)

	resp, raw = get(t, f, "/api/system/v1/groups/"+id, tok)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(raw), "devices_to_configure") {
		t.Errorf("detail: %d %s", resp.StatusCode, raw)
	}
	has("detail", raw, `"device":"INT0000001"`, `"members":[`+window+`,`+relay+`]`, types)

	resp, raw = send(t, f, http.MethodDelete, "/api/system/v1/groups/"+id, tok, `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("delete: %d %s", resp.StatusCode, raw)
	}
	has("delete", raw, `"deleted":1`, `"former_members":[`+window+`,`+relay+`]`)
	if resp, raw := get(t, f, "/api/system/v1/groups/"+id, tok); resp.StatusCode != http.StatusNotFound || errorCode(t, raw) != "unknown-group" {
		t.Errorf("deleted group: %d %s", resp.StatusCode, raw)
	}
	if _, raw := get(t, f, "/api/system/v1/groups/types", tok); true {
		has("types after delete", raw, `"assignable":[`+window+`,`+relay+`],"leftover":[]`)
	}
}

// TestSystemGroupDropsAMemberItsTypeCannotTake pins what a box does with a
// member id that is no candidate of the group's type: the write answers
// 200 and the group does not hold it. A client that trusts the status
// alone believes it assigned a device it did not.
func TestSystemGroupDropsAMemberItsTypeCannotTake(t *testing.T) {
	f := startFake(t, litefake.Options{GroupCandidates: groupCandidates()})
	tok := litefake.DefaultToken
	resp, raw := send(t, f, http.MethodPost, "/api/system/v1/groups", tok, `{"name":"Leer","type":"hmip.heating.group","members":["0000000000FFFF:1"]}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"members":[]`) || !strings.Contains(string(raw), `"devices_to_configure":[]`) {
		t.Fatalf("create with a member the type cannot take: %d %s", resp.StatusCode, raw)
	}
	// Without seeded candidates no type takes anything.
	bare := startSystemFake(t)
	_, raw = send(t, bare, http.MethodPost, "/api/system/v1/groups", tok, `{"name":"Leer","type":"hmip.heating.group","members":["0000000000AA01:1"]}`)
	if !strings.Contains(string(raw), `"members":[]`) {
		t.Errorf("a fake without candidates kept a member: %s", raw)
	}
}

// TestSystemServiceMessagesKnob pins what /service-messages reports.
func TestSystemServiceMessagesKnob(t *testing.T) {
	f := startSystemFake(t)
	f.SetServiceMessages([]litefake.ServiceMessage{{
		Interface: "HmIP-RF", Address: "VCU2128127", Channel: "0", Key: "LOW_BAT",
		Value: []byte("true"), Since: "2026-09-27T10:00:00Z", Seen: "event",
	}})
	_, raw := get(t, f, "/api/system/v1/service-messages", sysReadToken)
	if !strings.Contains(string(raw), `"count":1`) || !strings.Contains(string(raw), `"key":"LOW_BAT"`) {
		t.Errorf("service messages: %s", raw)
	}
}
