// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/pkg/litefake"
)

const noticeBody = `{"severity":"warning","title":{"de":"Broker weg","en":"Broker gone"}}`

// TestAddonNoticesNeedTheControlToken pins the credential: the add-on's
// control token as a Bearer and nothing else — not an API token, and not
// the control token a restart replaced.
func TestAddonNoticesNeedTheControlToken(t *testing.T) {
	f := startSystemFake(t)
	for name, token := range map[string]string{"no token": "", "an API token": litefake.DefaultToken} {
		if resp, raw := get(t, f, "/api/system/v1/addon-notices", token); resp.StatusCode != http.StatusUnauthorized || errorCode(t, raw) != "unauthenticated" {
			t.Errorf("%s: %d %s, want 401 unauthenticated", name, resp.StatusCode, raw)
		}
	}
	if resp, raw := get(t, f, "/api/system/v1/addon-notices", litefake.DefaultAddonControlToken); resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(raw), `"addon":"`+litefake.DefaultAddonID+`"`) || !strings.Contains(string(raw), `"max":20`) {
		t.Errorf("list with the control token: %d %s", resp.StatusCode, raw)
	}
	f.SetAddonControlToken("after-restart")
	if resp, _ := get(t, f, "/api/system/v1/addon-notices", litefake.DefaultAddonControlToken); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("replaced control token: %d, want 401", resp.StatusCode)
	}
}

// TestAddonNoticeLifecycle pins post, replace and clear: 201 for a new
// notice, 200 for a replaced one whose first posting time stays, 404 for
// clearing one the add-on does not have.
func TestAddonNoticeLifecycle(t *testing.T) {
	f := startSystemFake(t)
	const path = "/api/system/v1/addon-notices/health.mqtt"
	tok := litefake.DefaultAddonControlToken

	resp, raw := send(t, f, http.MethodPut, path, tok, noticeBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first put: %d %s, want 201", resp.StatusCode, raw)
	}
	posted := decode[litefake.AddonNotice](t, string(raw)).Posted
	resp, raw = send(t, f, http.MethodPut, path, tok, `{"severity":"error","title":{"de":"Broker weg","en":"Broker gone"}}`)
	replaced := decode[litefake.AddonNotice](t, string(raw))
	if resp.StatusCode != http.StatusOK || replaced.Severity != "error" || replaced.Posted != posted {
		t.Errorf("replacing put: %d %s, want 200, the new severity and posted %s", resp.StatusCode, raw, posted)
	}
	if got := f.AddonNotices(); len(got) != 1 || got[0].ID != "health.mqtt" {
		t.Errorf("notices = %+v, want the one notice", got)
	}
	if got := f.AddonNoticePuts(); got != 2 {
		t.Errorf("accepted puts = %d, want 2", got)
	}

	if resp, raw := send(t, f, http.MethodDelete, path, tok, `{}`); resp.StatusCode != http.StatusOK {
		t.Errorf("delete: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := send(t, f, http.MethodDelete, path, tok, `{}`); resp.StatusCode != http.StatusNotFound || errorCode(t, raw) != "not_found" {
		t.Errorf("second delete: %d %s, want 404 not_found", resp.StatusCode, raw)
	}
}

// TestAddonNoticeRefusals pins what the box refuses: a field it does not
// know (400), a notice that breaks a rule (422 naming the field), and
// the twenty-first notice (409).
func TestAddonNoticeRefusals(t *testing.T) {
	f := startSystemFake(t)
	tok := litefake.DefaultAddonControlToken
	const base = "/api/system/v1/addon-notices/"
	if resp, raw := send(t, f, http.MethodPut, base+"a", tok, `{"severity":"info","title":{"de":"x","en":"x"},"colour":"red"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: %d %s, want 400", resp.StatusCode, raw)
	}
	for field, tc := range map[string]struct{ id, body string }{
		"id":       {"Health:MQTT", noticeBody},
		"severity": {"a", `{"severity":"fatal","title":{"de":"x","en":"x"}}`},
		"title":    {"a", `{"severity":"info","title":{"de":"x"}}`},
		"text":     {"a", `{"severity":"info","title":{"de":"x","en":"x"},"text":{"en":"only english"}}`},
		"link":     {"a", `{"severity":"info","title":{"de":"x","en":"x"},"link":"https://example.invalid/"}`},
		"ttl":      {"a", `{"severity":"info","title":{"de":"x","en":"x"},"ttl":-1}`},
	} {
		resp, raw := send(t, f, http.MethodPut, base+tc.id, tok, tc.body)
		if resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, raw) != "invalid" || !strings.Contains(string(raw), field) {
			t.Errorf("%s: %d %s, want 422 invalid naming the field", field, resp.StatusCode, raw)
		}
	}
	for i := range 20 {
		if resp, raw := send(t, f, http.MethodPut, base+"n"+strconv.Itoa(i), tok, noticeBody); resp.StatusCode != http.StatusCreated {
			t.Fatalf("notice %d: %d %s", i, resp.StatusCode, raw)
		}
	}
	if resp, raw := send(t, f, http.MethodPut, base+"one-too-many", tok, noticeBody); resp.StatusCode != http.StatusConflict || errorCode(t, raw) != "too-many" {
		t.Errorf("twenty-first notice: %d %s, want 409 too-many", resp.StatusCode, raw)
	}
	if resp, raw := send(t, f, http.MethodPut, base+"n3", tok, noticeBody); resp.StatusCode != http.StatusOK {
		t.Errorf("replacing one of twenty: %d %s, want 200", resp.StatusCode, raw)
	}
	if resp, raw := send(t, f, http.MethodDelete, "/api/system/v1/addon-notices", tok, `{}`); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"cleared":20`) {
		t.Errorf("clear all: %d %s", resp.StatusCode, raw)
	}
}
