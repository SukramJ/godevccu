// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// End-to-end tests for the opt-in realism behaviours: separate
// interface listeners, TLS, the CCU error model, ReGa object ids and
// discovery. Each asserts both that the behaviour appears when asked
// for and that it stays absent by default — the pydevccu contract turns
// on the second half of every one of these.

package virtualccu_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/virtualccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// startRealistic boots a CCU with the given realism settings on
// ephemeral ports.
func startRealistic(t *testing.T, mutate func(cfg *virtualccu.Config)) *virtualccu.VirtualCCU {
	t.Helper()
	cfg := virtualccu.Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		JSONRPCPort: virtualccu.EphemeralPort,
		Username:    "Admin",
		Password:    "test",
		AuthEnabled: false,
		Devices:     []string{"HmIP-SWSD", "HM-LC-Sw1-Pl"},
	}
	mutate(&cfg)
	v, err := virtualccu.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	return v
}

// postJSON calls a JSON-RPC method against the web API.
func postJSON(t *testing.T, v *virtualccu.VirtualCCU, method string, params map[string]any) map[string]any {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "1.1",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "http://" + v.JSONRPCAddr().String() + "/api/homematic.cgi"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────
// Separate interface listeners
// ─────────────────────────────────────────────────────────────────

// TestInterfacePortsPartitionDevices is the core of the multi-interface
// model: each listener answers listDevices with only its own protocol
// family, the way a client connecting to 2001 sees no HomeMatic IP
// devices.
func TestInterfacePortsPartitionDevices(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.InterfacePorts = map[string]int{
			hmconst.InterfaceBidCosRF: virtualccu.EphemeralPort,
			hmconst.InterfaceHmIPRF:   virtualccu.EphemeralPort,
		}
	})

	rf := v.InterfaceAddr(hmconst.InterfaceBidCosRF)
	ip := v.InterfaceAddr(hmconst.InterfaceHmIPRF)
	if rf == nil || ip == nil {
		t.Fatal("interface listeners not bound")
	}
	if rf.String() == ip.String() {
		t.Fatal("both interfaces bound the same port")
	}

	rfTypes := deviceTypesVia(t, rf)
	ipTypes := deviceTypesVia(t, ip)

	if !containsPrefix(rfTypes, "HM-LC-Sw1-Pl") {
		t.Errorf("BidCos-RF is missing its own device: %v", rfTypes)
	}
	if containsPrefix(rfTypes, "HmIP-") {
		t.Errorf("BidCos-RF serves HomeMatic IP devices: %v", rfTypes)
	}
	if !containsPrefix(ipTypes, "HmIP-SWSD") {
		t.Errorf("HmIP-RF is missing its own device: %v", ipTypes)
	}
	if containsPrefix(ipTypes, "HM-LC-") {
		t.Errorf("HmIP-RF serves BidCos devices: %v", ipTypes)
	}
}

// Without InterfacePorts every device stays on the single endpoint.
func TestSingleEndpointByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	if v.InterfaceAddr(hmconst.InterfaceBidCosRF) != nil {
		t.Fatal("interface listener started without InterfacePorts")
	}
	types := deviceTypesVia(t, v.XMLRPCAddr())
	if !containsPrefix(types, "HmIP-SWSD") || !containsPrefix(types, "HM-LC-Sw1-Pl") {
		t.Fatalf("single endpoint must serve every device: %v", types)
	}
}

// TestListInterfacesReportsRealPorts pins that the inventory reports
// each interface on its own port, in the CCU's field set.
func TestListInterfacesReportsRealPorts(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.InterfacePorts = map[string]int{
			hmconst.InterfaceBidCosRF: virtualccu.EphemeralPort,
			hmconst.InterfaceHmIPRF:   virtualccu.EphemeralPort,
		}
	})
	resp := postJSON(t, v, "Interface.listInterfaces", nil)
	entries, ok := resp["result"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("result = %v, want 2 interfaces", resp["result"])
	}
	ports := map[string]float64{}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		for _, key := range []string{"name", "port", "info"} {
			if _, ok := entry[key]; !ok {
				t.Errorf("entry missing %q: %v", key, entry)
			}
		}
		// The real API reports these three fields and nothing else.
		if _, extra := entry["available"]; extra {
			t.Errorf("entry carries a field the CCU does not report: %v", entry)
		}
		ports[entry["name"].(string)] = entry["port"].(float64)
	}
	if ports[hmconst.InterfaceBidCosRF] == ports[hmconst.InterfaceHmIPRF] {
		t.Fatalf("interfaces advertise the same port: %v", ports)
	}
}

// ─────────────────────────────────────────────────────────────────
// TLS
// ─────────────────────────────────────────────────────────────────

func TestTLSListeners(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.TLS = virtualccu.TLSConfig{
			Enabled:     true,
			XMLRPCPort:  virtualccu.EphemeralPort,
			JSONRPCPort: virtualccu.EphemeralPort,
		}
	})

	xmlAddr := v.XMLRPCTLSAddr()
	if xmlAddr == nil {
		t.Fatal("no XML-RPC TLS listener")
	}
	// A client talking to a CCU's factory certificate skips
	// verification too.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed by design
	}}
	body := `<?xml version="1.0"?><methodCall><methodName>ping</methodName><params/></methodCall>`
	resp, err := client.Post("https://"+xmlAddr.String()+"/", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("HTTPS XML-RPC: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HTTPS XML-RPC status = %d, want 200", resp.StatusCode)
	}

	webAddr := v.TLSAddr()
	if webAddr == nil {
		t.Fatal("no web API TLS listener")
	}
	vResp, err := client.Get("https://" + webAddr.String() + "/VERSION")
	if err != nil {
		t.Fatalf("HTTPS web API: %v", err)
	}
	defer func() { _ = vResp.Body.Close() }()
	if vResp.StatusCode != http.StatusOK {
		t.Errorf("HTTPS /VERSION status = %d, want 200", vResp.StatusCode)
	}
}

func TestNoTLSByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	if v.TLSAddr() != nil || v.XMLRPCTLSAddr() != nil {
		t.Fatal("TLS listener started without being asked for")
	}
}

func TestHTTPSRedirect(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.TLS = virtualccu.TLSConfig{
			Enabled:     true,
			XMLRPCPort:  virtualccu.EphemeralPort,
			JSONRPCPort: virtualccu.EphemeralPort,
			Redirect:    true,
		}
	})

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Post("http://"+v.JSONRPCAddr().String()+"/api/homematic.cgi",
		"application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "https://") {
		t.Errorf("Location = %q, want an https target", loc)
	}

	// The API must report the redirect so a client can act on it —
	// asked over HTTPS, since the plaintext port now redirects.
	tlsClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed by design
	}}
	body := `{"jsonrpc":"1.1","method":"CCU.getHttpsRedirectEnabled","params":{},"id":1}`
	secure, err := tlsClient.Post("https://"+v.TLSAddr().String()+"/api/homematic.cgi",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("HTTPS POST: %v", err)
	}
	defer func() { _ = secure.Body.Close() }()
	var answer map[string]any
	if err := json.NewDecoder(secure.Body).Decode(&answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer["result"] != true {
		t.Errorf("getHttpsRedirectEnabled = %v, want true", answer["result"])
	}
}

// ─────────────────────────────────────────────────────────────────
// CCU error model
// ─────────────────────────────────────────────────────────────────

func TestCCUErrorModelEnvelope(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.ErrorModel = true
	})

	ok := postJSON(t, v, "CCU.getSerial", nil)
	if _, has := ok["jsonrpc"]; has {
		t.Errorf("success envelope still carries \"jsonrpc\": %v", ok)
	}
	if ok["version"] != "1.1" {
		t.Errorf("version = %v, want 1.1", ok["version"])
	}

	bad := postJSON(t, v, "No.suchMethod", nil)
	errObj, isObj := bad["error"].(map[string]any)
	if !isObj {
		t.Fatalf("error = %v, want an object", bad["error"])
	}
	if errObj["name"] != "JSONRPCError" {
		t.Errorf("name = %v, want JSONRPCError", errObj["name"])
	}
	if errObj["code"] != float64(401) {
		t.Errorf("code = %v, want 401 (method not found)", errObj["code"])
	}
}

func TestJSONRPC20EnvelopeByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	resp := postJSON(t, v, "CCU.getSerial", nil)
	if resp["jsonrpc"] != "1.1" {
		t.Errorf("default envelope must keep \"jsonrpc\": %v", resp)
	}
	if _, has := resp["version"]; has {
		t.Errorf("default envelope must not carry \"version\": %v", resp)
	}
}

// TestPrivilegeLevelsRejectUnauthenticated pins the access-denied path
// that the 2.0 codes never triggered.
func TestPrivilegeLevelsRejectUnauthenticated(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.AuthEnabled = true
		cfg.Realism.ErrorModel = true
	})

	denied := postJSON(t, v, "SysVar.getAll", nil)
	errObj, ok := denied["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error without a session, got %v", denied)
	}
	if errObj["code"] != float64(400) {
		t.Errorf("code = %v, want 400 (access denied)", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "access denied") {
		t.Errorf("message = %q, want the CCU wording", msg)
	}

	// A NONE-level method stays reachable.
	if resp := postJSON(t, v, "system.listMethods", nil); resp["error"] != nil {
		t.Errorf("system.listMethods must not require a session: %v", resp["error"])
	}
}

// ─────────────────────────────────────────────────────────────────
// ReGa object ids
// ─────────────────────────────────────────────────────────────────

// TestRegaIDsLinkRoomsToChannels covers the cross-reference that is
// broken without the object ids: a room reports channel ids, and a
// client matches them against the ids in listAllDetail.
//
// A CCU sends those ids as strings holding a number — `"id": "18470"`,
// `"channelIds": ["38552", …]`, read back from 3.87. The distinction
// matters twice over: a client whose DTO says string cannot decode a
// JSON number and loses the whole document, and asserting merely
// "not a textual address" passes for a number just as happily as for
// the correct string.
func TestRegaIDsLinkRoomsToChannels(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.RegaIDs = true
		cfg.Realism.JSONSchema = true
	})

	detail := postJSON(t, v, "Device.listAllDetail", nil)
	devices, ok := detail["result"].([]any)
	if !ok || len(devices) == 0 {
		t.Fatalf("no devices: %v", detail["result"])
	}
	first := devices[0].(map[string]any)
	deviceID, isString := first["id"].(string)
	if !isString {
		t.Fatalf("device id is %T (%v), want a string as a CCU sends it", first["id"], first["id"])
	}
	if _, err := strconv.Atoi(deviceID); err != nil {
		t.Fatalf("device id %q is not a numeric object id", deviceID)
	}
	if deviceID == first["address"] {
		t.Fatalf("device id is still the address: %q", deviceID)
	}

	channels, _ := first["channels"].([]any)
	if len(channels) == 0 {
		t.Fatal("device has no channels")
	}
	channel := channels[0].(map[string]any)
	channelAddress := channel["address"].(string)
	channelID, isString := channel["id"].(string)
	if !isString {
		t.Fatalf("channel id is %T (%v), want a string", channel["id"], channel["id"])
	}

	v.State().AddRoom("Wohnzimmer", "", []string{channelAddress}, 0)

	rooms := postJSON(t, v, "Room.getAll", nil)
	entries := rooms["result"].([]any)
	var found bool
	for _, raw := range entries {
		room := raw.(map[string]any)
		ids, _ := room["channelIds"].([]any)
		for _, id := range ids {
			if id == channelID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("room channelIds do not reference the channel id %q", channelID)
	}
}

func TestAddressIDsByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	detail := postJSON(t, v, "Device.listAllDetail", nil)
	devices := detail["result"].([]any)
	first := devices[0].(map[string]any)
	if _, isString := first["id"].(string); !isString {
		t.Fatalf("default id must stay the textual address, got %v", first["id"])
	}
}

// ─────────────────────────────────────────────────────────────────
// Discovery
// ─────────────────────────────────────────────────────────────────

func TestUPnPDescriptionServed(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.Discovery = true
	})
	resp, err := http.Get("http://" + v.JSONRPCAddr().String() + "/upnp/basic_dev.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"<friendlyName>", "<serialNumber>", "uuid:", "urn:schemas-upnp-org:device:Basic:1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("description missing %q:\n%s", want, body)
		}
	}
}

func TestNoUPnPDescriptionByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	resp, err := http.Get("http://" + v.JSONRPCAddr().String() + "/upnp/basic_dev.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("UPnP description served without discovery enabled")
	}
}

// ─────────────────────────────────────────────────────────────────
// Backup lifecycle
// ─────────────────────────────────────────────────────────────────

// TestBackupReachesCompleted pins the lifecycle that never advanced:
// a started backup stayed "running" forever and the download was a
// permanent 404.
func TestBackupReachesCompleted(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.BackupAPI = true
	})

	v.State().StartBackup()
	if got := v.State().BackupStatus().Status; got != "running" {
		t.Fatalf("status right after start = %q, want running", got)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if v.State().BackupStatus().Status == "completed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	status := v.State().BackupStatus()
	if status.Status != "completed" {
		t.Fatalf("backup never completed, still %q", status.Status)
	}
	if !strings.HasSuffix(status.Filename, ".sbk") {
		t.Errorf("filename = %q, want a .sbk archive", status.Filename)
	}

	resp, err := http.Get("http://" + v.JSONRPCAddr().String() + "/api/backup/tarfile.cgi")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", resp.StatusCode)
	}
}

func TestBackupStaysRunningByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	v.State().StartBackup()
	time.Sleep(400 * time.Millisecond)
	if got := v.State().BackupStatus().Status; got != "running" {
		t.Fatalf("status = %q, want running (no automation by default)", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

// deviceTypesVia lists the device types an XML-RPC endpoint serves.
func deviceTypesVia(t *testing.T, addr net.Addr) []string {
	t.Helper()
	client := xmlrpc.NewClient("http://" + addr.String() + "/")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := client.Call(ctx, "listDevices", nil)
	if err != nil {
		t.Fatalf("listDevices via %s: %v", addr, err)
	}
	arr, ok := result.(xmlrpc.ArrayValue)
	if !ok {
		t.Fatalf("listDevices returned %T", result)
	}
	types := make([]string, 0, len(arr))
	for _, entry := range arr {
		record, ok := xmlrpc.ToAny(entry).(map[string]any)
		if !ok {
			continue
		}
		if typeStr, ok := record["TYPE"].(string); ok {
			types = append(types, typeStr)
		}
	}
	return types
}

// containsPrefix reports whether any entry starts with prefix.
func containsPrefix(values []string, prefix string) bool {
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────
// Basic authentication
// ─────────────────────────────────────────────────────────────────

// TestBasicAuthLeavesLoopbackFixturesAlone is the half of the gate an
// end-to-end test can reach: every listener a test binds is loopback,
// and a CCU exempts loopback from its auth block. The rejecting half is
// covered by the gate's own tests, which can forge a remote address.
func TestBasicAuthLeavesLoopbackFixturesAlone(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.AuthEnabled = true
		cfg.Realism.BasicAuth = true
	})

	body := `<?xml version="1.0"?><methodCall><methodName>ping</methodName><params/></methodCall>`
	resp, err := http.Post("http://"+v.XMLRPCAddr().String()+"/", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loopback XML-RPC status = %d, want 200 — the auth exemption is not wired", resp.StatusCode)
	}
}

// The web API authenticates by session, so it must not gain a
// basic-auth challenge: a CCU switches the auth block back off for its
// WebUI ports.
func TestBasicAuthDoesNotCoverTheWebAPI(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.AuthEnabled = true
		cfg.Realism.BasicAuth = true
	})

	resp, err := http.Get("http://" + v.JSONRPCAddr().String() + "/VERSION")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if challenge := resp.Header.Get("WWW-Authenticate"); challenge != "" {
		t.Errorf("web API issued a basic-auth challenge %q", challenge)
	}
}

// Without AuthEnabled the flag stays inert — a CCU only includes its
// auth configuration when authentication is switched on.
func TestBasicAuthRequiresAuthEnabled(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.AuthEnabled = false
		cfg.Realism.BasicAuth = true
	})
	if rpc := v.RPC(); rpc == nil {
		t.Fatal("no RPC facade")
	}
	body := `<?xml version="1.0"?><methodCall><methodName>ping</methodName><params/></methodCall>`
	resp, err := http.Post("http://"+v.XMLRPCAddr().String()+"/", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
