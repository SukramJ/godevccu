// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Extra HTTP surface of a real CCU.
//
// Besides the JSON-RPC endpoint a CCU serves a small set of CGI scripts
// that clients and backup tools talk to directly: /api/backup/* for
// session-less backup handling and script execution, and
// /upnp/basic_dev.cgi for the UPnP device description an SSDP responder
// points at. Both are opt-in — see virtualccu.Realism.

// ExtraRoutes selects which of the additional endpoints to serve.
type ExtraRoutes struct {
	// BackupAPI serves /api/backup/{login,version,run-script,tarfile}.cgi.
	BackupAPI bool
	// UPnP serves /upnp/basic_dev.cgi, the device description an SSDP
	// M-SEARCH response points at.
	UPnP bool
}

// SetExtraRoutes configures the additional endpoints. Call before
// [Server.Start].
func (s *Server) SetExtraRoutes(r ExtraRoutes) { s.extraRoutes = r }

// registerExtraRoutes wires the enabled endpoints into mux.
func (s *Server) registerExtraRoutes(mux *http.ServeMux) {
	if s.extraRoutes.BackupAPI {
		mux.HandleFunc("/api/backup/login.cgi", s.handleBackupLogin)
		mux.HandleFunc("/api/backup/version.cgi", s.handleBackupVersion)
		mux.HandleFunc("/api/backup/run-script.cgi", s.handleBackupRunScript)
		mux.HandleFunc("/api/backup/tarfile.cgi", s.handleBackupTarfile)
	}
	if s.extraRoutes.UPnP {
		mux.HandleFunc("/upnp/basic_dev.cgi", s.handleUPnPDescription)
	}
}

// handleBackupLogin authenticates and returns a session id as plain
// text, the way the backup CGI does — it predates the JSON-RPC API and
// speaks its own minimal protocol.
func (s *Server) handleBackupLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" && password == "" {
		body, _ := io.ReadAll(io.LimitReader(r.Body, DefaultRequestLimit))
		var payload map[string]string
		if json.Unmarshal(body, &payload) == nil {
			username, password = payload["username"], payload["password"]
		}
	}
	id := s.session.Login(username, password)
	if id == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, id)
}

// handleBackupVersion reports the firmware version as plain text.
func (s *Server) handleBackupVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, s.state.BackendInfo().Version)
}

// handleBackupRunScript executes a ReGa script posted as the request
// body and returns its output — the escape hatch backup tooling uses
// when the JSON-RPC session handling is in the way.
func (s *Server) handleBackupRunScript(w http.ResponseWriter, r *http.Request) {
	if !s.backupAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if s.handlers == nil || s.handlers.ReGa == nil {
		http.Error(w, "No script engine", http.StatusNotImplemented)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, DefaultRequestLimit))
	result := s.handlers.ReGa.Execute(string(body))
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, result.Output)
}

// handleBackupTarfile streams the finished archive. Before the backup
// completes a CCU answers 404, which is what the status script's
// "running" state means for a downloader.
func (s *Server) handleBackupTarfile(w http.ResponseWriter, r *http.Request) {
	if !s.backupAuthorized(r) {
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

// backupAuthorized accepts the session id in the query string or in the
// Authorization header, matching how the CGI scripts are called.
func (s *Server) backupAuthorized(r *http.Request) bool {
	if !s.session.AuthEnabled() {
		return true
	}
	if sid := r.URL.Query().Get("sid"); sid != "" && s.session.Validate(sid) {
		return true
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return s.session.Validate(strings.TrimPrefix(auth, "Bearer "))
	}
	return false
}

// upnpDevice is the UPnP basic device description a CCU serves. The
// field set is what a discovering client reads to identify the central:
// friendly name, manufacturer, model and the serial that ties the
// discovery result to the device.
type upnpDevice struct {
	XMLName     xml.Name `xml:"urn:schemas-upnp-org:device-1-0 root"`
	SpecVersion struct {
		Major int `xml:"major"`
		Minor int `xml:"minor"`
	} `xml:"specVersion"`
	Device struct {
		DeviceType       string `xml:"deviceType"`
		FriendlyName     string `xml:"friendlyName"`
		Manufacturer     string `xml:"manufacturer"`
		ManufacturerURL  string `xml:"manufacturerURL"`
		ModelDescription string `xml:"modelDescription"`
		ModelName        string `xml:"modelName"`
		ModelNumber      string `xml:"modelNumber"`
		SerialNumber     string `xml:"serialNumber"`
		UDN              string `xml:"UDN"`
		PresentationURL  string `xml:"presentationURL"`
	} `xml:"device"`
}

// handleUPnPDescription serves the device description SSDP points at.
func (s *Server) handleUPnPDescription(w http.ResponseWriter, _ *http.Request) {
	info := s.state.BackendInfo()
	serial := s.state.Serial()

	var doc upnpDevice
	doc.SpecVersion.Major = 1
	doc.Device.DeviceType = "urn:schemas-upnp-org:device:Basic:1"
	doc.Device.FriendlyName = info.Hostname
	doc.Device.Manufacturer = "eQ-3"
	doc.Device.ManufacturerURL = "https://www.eq-3.de"
	doc.Device.ModelDescription = info.Product
	doc.Device.ModelName = info.Product
	doc.Device.ModelNumber = info.Version
	doc.Device.SerialNumber = serial
	doc.Device.UDN = "uuid:" + upnpUUID(serial)
	doc.Device.PresentationURL = "/"

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

// upnpUUID derives a stable UUID-shaped identifier from the serial, the
// way a CCU derives its UDN from its own serial number.
func upnpUUID(serial string) string {
	padded := serial
	for len(padded) < 12 {
		padded += "0"
	}
	padded = padded[:12]
	return "30303030-3030-3030-3030-" + padded
}
