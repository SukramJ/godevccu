// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/godevccu/internal/session"
	"github.com/SukramJ/godevccu/internal/state"
)

// DefaultRequestLimit caps the body size in bytes.
const DefaultRequestLimit = 4 * 1024 * 1024

// JSONRPCVersion is the response version field. CCU clients (in
// particular aiohomematic/gohomematic) require "1.1" with both result and error
// present even on success.
const JSONRPCVersion = "1.1"

// Request is the payload received from the client.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      any    `json:"id,omitempty"`
	// Some clients put the session id at the top level rather than in
	// params; preserved verbatim so we can still extract it.
	SessionID any `json:"_session_id_,omitempty"`
}

// Response mirrors the envelope expected by aiohomematic/gohomematic.
type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	Result  any            `json:"result"`
	Error   map[string]any `json:"error"`
	ID      any            `json:"id"`
}

// Server is the JSON-RPC HTTP server.
type Server struct {
	logger   *slog.Logger
	addr     string
	handlers *Handlers
	session  *session.Manager
	state    *state.Manager

	httpSrv  *http.Server
	listener net.Listener

	mu      sync.Mutex
	running bool
	methods map[string]HandlerFunc

	// ready models the CCU boot state. A real CCU answers its web API
	// (Device.listAllDetail and friends) with http 503 for the first ~minute
	// after power-on while ReGaHss warms up, and /ise/checkrega.cgi returns a
	// body other than "OK" until then. While ready is false the JSON-RPC
	// surface returns 503 and checkrega reports "not ready"; flip it with
	// [Server.SetReady]. Defaults to true so existing fixtures see an
	// immediately-serving CCU.
	ready atomic.Bool

	tls tlsState

	// extraRoutes selects the additional CGI endpoints; see
	// extra_routes.go.
	extraRoutes ExtraRoutes

	// ccuErrorModel switches to the CCU's 1.1 envelope, its numeric
	// error codes and its privilege levels; see ccuerrors.go.
	ccuErrorModel bool

	// httpsRedirectPort is non-zero when the plaintext listener should
	// answer 302 towards its HTTPS twin; see [Server.SetHTTPSRedirect].
	httpsRedirectPort atomic.Int64
}

// Config configures [NewServer].
type Config struct {
	Address  string
	Handlers *Handlers
	Logger   *slog.Logger
}

// NewServer constructs a Server.
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		logger:   logger,
		addr:     cfg.Address,
		handlers: cfg.Handlers,
		session:  cfg.Handlers.Session,
		state:    cfg.Handlers.State,
		methods:  cfg.Handlers.Methods(),
	}
	s.ready.Store(true)
	return s
}

// SetReady toggles the simulated CCU boot state. Pass false to model a CCU
// that is still warming up (JSON-RPC answers 503, /ise/checkrega.cgi reports
// "not ready"); pass true once it has "finished booting". Safe for concurrent
// use.
func (s *Server) SetReady(ready bool) { s.ready.Store(ready) }

// Ready reports the current simulated boot state.
func (s *Server) Ready() bool { return s.ready.Load() }

// LocalAddr returns the bound address (only valid after Start).
func (s *Server) LocalAddr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Start begins serving on the configured address.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("jsonrpc: server already running")
	}
	mux := s.routes()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("jsonrpc: listen: %w", err)
	}
	s.listener = ln
	srv := &http.Server{
		Handler:           s.redirectGate(mux),
		ReadHeaderTimeout: 30 * time.Second,
	}
	s.httpSrv = srv
	s.running = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("jsonrpc: serve failed", "err", err)
		}
	}()
	return nil
}

// Stop tears the server down.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	srv := s.httpSrv
	s.httpSrv = nil
	s.listener = nil
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// ─────────────────────────────────────────────────────────────────
// JSON-RPC
// ─────────────────────────────────────────────────────────────────

// handleCheckRega serves the CCU's readiness probe. The OCCU WebUI boot page
// polls this in a loop and only proceeds once the body is the literal "OK";
// while the simulated CCU is still booting it returns a non-OK body so a
// readiness-gated client keeps waiting. Always HTTP 200 (lighttpd is up), the
// body carries the signal — matching the real /ise/checkrega.cgi contract.
func (s *Server) handleCheckRega(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.ready.Load() {
		_, _ = io.WriteString(w, "OK")
		return
	}
	_, _ = io.WriteString(w, "ReGaHss not ready")
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	// Model the boot window: a CCU whose ReGaHss has not finished starting
	// answers the web API with http 503 ("internal backend exception"). A
	// readiness-gated client must not call this until checkrega reports OK.
	if !s.ready.Load() {
		http.Error(w, "service not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleJSONRPCPost(w, r)
	case http.MethodGet:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error":    "Use POST to call JSON-RPC methods",
			"endpoint": "/api/homematic.cgi",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleJSONRPCPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, DefaultRequestLimit))
	if err != nil {
		writeJSON(w, http.StatusOK, errorResponse(nil, ErrParse(err.Error())))
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		// Batch request.
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			writeJSON(w, http.StatusOK, errorResponse(nil, ErrParse(err.Error())))
			return
		}
		if len(batch) == 0 {
			writeJSON(w, http.StatusOK, errorResponse(nil, ErrInvalid("Empty batch request")))
			return
		}
		responses := make([]map[string]any, 0, len(batch))
		for _, raw := range batch {
			if resp := s.processOne(r.Context(), raw); resp != nil {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}
	resp := s.processOne(r.Context(), body)
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// processOne handles a single request body. Returns nil for
// notifications (no response).
func (s *Server) processOne(ctx context.Context, raw []byte) map[string]any {
	var req struct {
		JSONRPC   string          `json:"jsonrpc"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
		ID        any             `json:"id"`
		SessionID json.RawMessage `json:"_session_id_,omitempty"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.failure(nil, ErrParse(err.Error()))
	}
	// A CCU never inspects the version member, so with its error model
	// active an absent version is accepted too.
	versionOmitted := s.ccuErrorModel && req.JSONRPC == ""
	if req.JSONRPC != "1.1" && req.JSONRPC != "2.0" && !versionOmitted {
		return s.failure(req.ID, ErrInvalid("Invalid JSON-RPC version"))
	}
	if req.Method == "" {
		return s.failure(req.ID, ErrInvalid("Method must be a non-empty string"))
	}

	var params map[string]any
	if len(req.Params) > 0 {
		switch req.Params[0] {
		case '{':
			_ = json.Unmarshal(req.Params, &params)
		case '[':
			var arr []any
			_ = json.Unmarshal(req.Params, &arr)
			params = map[string]any{"args": arr}
		}
	}
	if params == nil {
		params = map[string]any{}
	}

	// Authentication and privilege level.
	if err := s.authorize(req.Method, raw, params); err != nil {
		return s.failure(req.ID, err)
	}

	handler, ok := s.methods[req.Method]
	if !ok {
		return s.failure(req.ID, ErrMethod(req.Method))
	}

	result, err := handler(ctx, params)
	if req.ID == nil {
		// Notification — discard the result.
		return nil
	}
	if err != nil {
		var jrErr *Error
		if errors.As(err, &jrErr) {
			return s.failure(req.ID, jrErr)
		}
		return s.failure(req.ID, ErrInternal(err.Error()))
	}
	return s.success(req.ID, result)
}

// authorize enforces authentication. With the CCU error model active it
// compares the method's declared privilege level against the session's:
// a valid session is an administrator (the simulator has a single
// account), no session is NONE. Otherwise the historical public-method
// set decides.
func (s *Server) authorize(method string, raw []byte, params map[string]any) *Error {
	if !s.session.AuthEnabled() {
		return nil
	}
	valid := s.session.Validate(extractSessionID(raw, params))

	if !s.ccuErrorModel {
		if _, public := PublicMethods[method]; public || valid {
			return nil
		}
		return ErrSession("Session expired or invalid")
	}

	required := metaFor(method).level
	if required == "NONE" {
		return nil
	}
	have := levelNone
	if valid {
		have = levelAdmin
	}
	if levelValue(required) > have {
		return ErrAccessDenied(required, have)
	}
	return nil
}

// SetCCUErrorModel switches the envelope and error objects to the CCU's
// own form and enables privilege-level enforcement. Call before Start.
func (s *Server) SetCCUErrorModel(enabled bool) { s.ccuErrorModel = enabled }

// extractSessionID examines the request body and the (already parsed)
// params for a session id. Mirrors pydevccu's _extract_session_id.
func extractSessionID(raw []byte, params map[string]any) string {
	var top struct {
		SessionID json.RawMessage `json:"_session_id_,omitempty"`
	}
	_ = json.Unmarshal(raw, &top)
	if id := decodeSessionID(top.SessionID); id != "" {
		return id
	}
	if v, ok := params["_session_id_"]; ok {
		switch x := v.(type) {
		case string:
			return parseStringifiedSessionID(x)
		case map[string]any:
			if inner, ok := x["_session_id_"].(string); ok {
				return parseStringifiedSessionID(inner)
			}
		}
	}
	return ""
}

func decodeSessionID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return parseStringifiedSessionID(s)
	case '{':
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			if s, ok := m["_session_id_"].(string); ok {
				return parseStringifiedSessionID(s)
			}
		}
	}
	return ""
}

// parseStringifiedSessionID handles aiohomematic/gohomematic's quirk of nesting a
// dict-shaped string ("{'_session_id_': 'abc'}") inside the JSON
// string. Matches pydevccu's ast.literal_eval branch.
func parseStringifiedSessionID(s string) string {
	if len(s) > 0 && s[0] == '{' {
		// Try Python-style → JSON-style by swapping single quotes.
		swapped := []rune(s)
		for i, r := range swapped {
			if r == '\'' {
				swapped[i] = '"'
			}
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(string(swapped)), &m); err == nil {
			if inner, ok := m["_session_id_"].(string); ok {
				return inner
			}
		}
	}
	return s
}

// ─────────────────────────────────────────────────────────────────
// HTTP endpoints (non JSON-RPC)
// ─────────────────────────────────────────────────────────────────

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if s.session.AuthEnabled() && !s.session.Validate(r.URL.Query().Get("sid")) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	status := s.state.BackupStatus()
	if status.Status != "completed" {
		http.Error(w, "No backup available", http.StatusNotFound)
		return
	}
	data := s.state.BackupData()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+status.Filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	if s.session.AuthEnabled() && !s.session.Validate(r.URL.Query().Get("sid")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, DefaultRequestLimit))
	var payload map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	switch payload["action"] {
	case "checkUpdate":
		info := s.state.UpdateInfo()
		writeJSON(w, http.StatusOK, map[string]any{
			"currentFirmware":   info.CurrentFirmware,
			"availableFirmware": info.AvailableFirmware,
			"updateAvailable":   info.UpdateAvailable,
		})
	case "triggerUpdate":
		writeJSON(w, http.StatusOK, map[string]any{"success": s.state.TriggerUpdate()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown action"})
	}
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	info := s.state.BackendInfo()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("VERSION=" + info.Version + "\nPRODUCT=" + info.Product + "\n"))
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func successResponse(id any, result any) map[string]any {
	return map[string]any{
		"jsonrpc": JSONRPCVersion,
		"result":  result,
		"error":   nil,
		"id":      id,
	}
}

func errorResponse(id any, err *Error) map[string]any {
	return map[string]any{
		"jsonrpc": JSONRPCVersion,
		"result":  nil,
		"error":   err.MarshalDict(),
		"id":      id,
	}
}

// ccuSuccessResponse and ccuErrorResponse render the envelope a real
// CCU sends: the version lives in "version", not "jsonrpc".
func ccuSuccessResponse(id any, result any) map[string]any {
	out := map[string]any{
		"version": JSONRPCVersion,
		"result":  result,
		"error":   nil,
	}
	if id != nil {
		out["id"] = id
	}
	return out
}

func ccuErrorResponse(id any, err *Error) map[string]any {
	out := map[string]any{
		"version": JSONRPCVersion,
		"result":  nil,
		"error":   err.marshalCCU(),
	}
	if id != nil {
		out["id"] = id
	}
	return out
}

// success and failure pick the envelope for the configured model.
func (s *Server) success(id any, result any) map[string]any {
	if s.ccuErrorModel {
		return ccuSuccessResponse(id, result)
	}
	return successResponse(id, result)
}

func (s *Server) failure(id any, err *Error) map[string]any {
	if s.ccuErrorModel {
		return ccuErrorResponse(id, err)
	}
	return errorResponse(id, err)
}
