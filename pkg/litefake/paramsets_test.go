// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/pkg/litefake"
)

type paramsetsAnswer struct {
	Keys      map[string]string                     `json:"keys"`
	Paramsets map[string]map[string]json.RawMessage `json:"paramsets"`
	Errors    map[string]string                     `json:"errors"`
}

// TestParamsetsAnswerManyAddressesAtOnce pins the route's answer: a
// channel maps to the key of its description, the description is held
// under that key, and an address the process refuses is named in errors
// without failing the request.
func TestParamsetsAnswerManyAddressesAtOnce(t *testing.T) {
	f := startFake(t, litefake.Options{})
	resp, raw := send(t, f, http.MethodPost, "/api/rpc/v1/paramsets/"+bidcosRF, litefake.DefaultToken,
		`{"addresses":["`+switchChannel+`","VCU0000321:0","NOSUCHDEVICE:1"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paramsets: %d %s", resp.StatusCode, raw)
	}
	ans := decode[paramsetsAnswer](t, string(raw))
	key := ans.Keys[switchChannel]
	if !strings.HasPrefix(key, bidcosRF+"/") || !strings.HasSuffix(key, "/VALUES") || strings.Count(key, "/") != 5 {
		t.Errorf("key of %s = %q, want <interface>/<type>/<firmware>/<version>/<channel type>/VALUES", switchChannel, key)
	}
	state, ok := ans.Paramsets[key]["STATE"]
	if !ok || !strings.Contains(string(state), `"TYPE":"BOOL"`) {
		t.Errorf("description under %q lacks STATE as a BOOL: %s", key, state)
	}
	if _, ok := ans.Keys["VCU0000321:0"]; !ok {
		t.Errorf("the maintenance channel has no key: %s", raw)
	}
	if msg := ans.Errors["NOSUCHDEVICE:1"]; !strings.HasPrefix(msg, "RPC fault (code: ") {
		t.Errorf("error for an unknown address = %q, want the process's fault", msg)
	}
	if _, ok := ans.Keys["NOSUCHDEVICE:1"]; ok {
		t.Error("an unknown address has a key")
	}
}

// TestParamsetsRefusals pins the route's own refusals.
func TestParamsetsRefusals(t *testing.T) {
	f := startFake(t, litefake.Options{})
	const one = `{"addresses":["` + switchChannel + `"]}`
	for name, tc := range map[string]struct {
		path, token, body string
		status            int
	}{
		"unknown interface": {"/api/rpc/v1/paramsets/NoSuch-RF", litefake.DefaultToken, one, http.StatusNotFound},
		"no addresses":      {"/api/rpc/v1/paramsets/" + bidcosRF, litefake.DefaultToken, `{"addresses":[]}`, http.StatusBadRequest},
		"no credential":     {"/api/rpc/v1/paramsets/" + bidcosRF, "", one, http.StatusUnauthorized},
	} {
		if resp, raw := send(t, f, http.MethodPost, tc.path, tc.token, tc.body); resp.StatusCode != tc.status {
			t.Errorf("%s: %d %s, want %d", name, resp.StatusCode, raw, tc.status)
		}
	}
	if err := f.SetInterfaceDown(bidcosRF, true); err != nil {
		t.Fatal(err)
	}
	if resp, raw := send(t, f, http.MethodPost, "/api/rpc/v1/paramsets/"+bidcosRF, litefake.DefaultToken, one); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("interface down: %d %s, want 503", resp.StatusCode, raw)
	}
	f.Deviate(litefake.DeviateNoParamsetsRoute, true)
	if resp, raw := send(t, f, http.MethodPost, "/api/rpc/v1/paramsets/"+bidcosRF, litefake.DefaultToken, one); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a box without the route: %d %s, want 404", resp.StatusCode, raw)
	}
}
