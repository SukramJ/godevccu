// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// The paramset description route.
//
// POST /api/rpc/v1/paramsets/{interface} answers the paramset descriptions
// of many addresses at once, each description once by its key. The fake
// reads every description from its interface process when it is asked;
// it keeps no cache of its own, which a client cannot tell apart.

const (
	maxParamsetAddresses = 2000
	paramsetValues       = "VALUES"
)

type paramsetsBody struct {
	Addresses []string `json:"addresses"`
	Paramset  string   `json:"paramset"`
}

// paramsetsAnswer maps each address to the key of its description, holds
// each description once, and names the addresses the process refused.
type paramsetsAnswer struct {
	Keys      map[string]string          `json:"keys"`
	Paramsets map[string]json.RawMessage `json:"paramsets"`
	Errors    map[string]string          `json:"errors"`
}

func (f *Fake) handleParamsets(w http.ResponseWriter, r *http.Request) {
	if f.deviates(DeviateNoParamsetsRoute) {
		writeError(w, http.StatusNotFound, "not_found", "no such API path")
		return
	}
	if _, _, ok := f.authorize(w, r, scopeRPCRead, false); !ok {
		return
	}
	iface := r.PathValue("iface")
	st, known := f.ifaceSnapshot(iface)
	if !known {
		writeError(w, http.StatusNotFound, "unknown-interface", "no such interface: "+iface)
		return
	}
	var body paramsetsBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request", "invalid body")
		return
	}
	if len(body.Addresses) == 0 || len(body.Addresses) > maxParamsetAddresses {
		writeError(w, http.StatusBadRequest, "bad-request", "1 to 2000 addresses")
		return
	}
	if body.Paramset == "" {
		body.Paramset = paramsetValues
	}
	if st.down {
		f.writeDown(w, iface, "marked down")
		return
	}
	ans := paramsetsAnswer{Keys: map[string]string{}, Paramsets: map[string]json.RawMessage{}, Errors: map[string]string{}}
	call := func(method string, params ...xmlrpc.Value) (xmlrpc.Value, string) {
		resp, err := forward(r.Context(), st.daemonURL, &xmlrpc.MethodCall{Method: method, Params: params})
		switch {
		case err != nil:
			return nil, err.Error()
		case resp.Fault != nil:
			return nil, fmt.Sprintf("RPC fault (code: %d, message: %s)", resp.Fault.Code, resp.Fault.Message)
		case len(resp.Params) == 0:
			return nil, "empty answer"
		}
		return resp.Params[0], ""
	}
	for _, addr := range body.Addresses {
		desc, fault := call("getParamsetDescription", xmlrpc.StringValue(addr), xmlrpc.StringValue(body.Paramset))
		if fault != "" {
			ans.Errors[addr] = fault
			continue
		}
		key, fault := paramsetKey(call, iface, addr, body.Paramset)
		if fault != "" {
			ans.Errors[addr] = fault
			continue
		}
		ans.Keys[addr] = key
		if _, held := ans.Paramsets[key]; !held {
			ans.Paramsets[key] = jsonValue(desc, false)
		}
	}
	writeJSON(w, http.StatusOK, ans)
}

// paramsetKey builds the key a description is held under:
// "<interface>/<device type>/<firmware>/<version>/<channel type>/<paramset>",
// the channel type empty for a paramset of the device itself. The fake
// takes the version from the description of the address asked; which
// description the box takes it from is not something its documents say.
func paramsetKey(call func(string, ...xmlrpc.Value) (xmlrpc.Value, string), iface, addr, paramset string) (key, fault string) {
	own, fault := call("getDeviceDescription", xmlrpc.StringValue(addr))
	if fault != "" {
		return "", fault
	}
	device, channelType := own, ""
	if parent, _, isChannel := strings.Cut(addr, ":"); isChannel {
		channelType = descString(own, "TYPE")
		if device, fault = call("getDeviceDescription", xmlrpc.StringValue(parent)); fault != "" {
			return "", fault
		}
	}
	return strings.Join([]string{
		iface, descString(device, "TYPE"), descString(device, "FIRMWARE"), descString(own, "VERSION"), channelType, paramset,
	}, "/"), ""
}

// descString reads a member of a device description as text.
func descString(desc xmlrpc.Value, name string) string {
	st, ok := xmlrpc.AsStruct(desc)
	if !ok {
		return ""
	}
	v, ok := st.Get(name)
	if !ok {
		return ""
	}
	if s, ok := xmlrpc.AsString(v); ok {
		return s
	}
	if n, ok := xmlrpc.AsInt(v); ok {
		return fmt.Sprint(n)
	}
	return ""
}
