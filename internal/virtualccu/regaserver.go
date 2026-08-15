// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/binrpc"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/regavm"
)

// The ReGa script endpoint.
//
// A CCU serves HomeMatic Script on its own port under /tclrega.exe: a
// client POSTs a script and gets its output back wrapped in an XML
// envelope. ccu-jack talks to a central exclusively this way, so
// without the endpoint it cannot attach at all.
//
// The port is opt-in through [Config.RegaScriptPort]; pydevccu has no
// such endpoint.

// PortRegaScript is the port a CCU serves scripts on.
const PortRegaScript = 8181

// regaServer serves the script endpoint.
type regaServer struct {
	srv      *http.Server
	listener net.Listener
}

// startRegaScript brings the endpoint up.
func (v *VirtualCCU) startRegaScript() error {
	port := v.cfg.RegaScriptPort
	if port == 0 {
		return nil
	}
	bindPort := port
	if bindPort < 0 {
		bindPort = 0
	}

	interpreter := v.newInterpreter()
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		serveRegaScript(w, r, interpreter, v.logger)
	}
	// A CCU accepts the script under both spellings.
	mux.HandleFunc("/tclrega.exe", handler)
	mux.HandleFunc("/rega.exe", handler)

	listener, err := net.Listen("tcp", net.JoinHostPort(v.cfg.Host, strconv.Itoa(bindPort)))
	if err != nil {
		return fmt.Errorf("virtualccu: rega script listen: %w", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 30 * time.Second}
	v.regaScript = &regaServer{srv: srv, listener: listener}
	if addr, ok := listener.Addr().(*net.TCPAddr); ok && addr != nil {
		v.cfg.RegaScriptPort = addr.Port
	}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			v.logger.Error("virtualccu: rega script serve failed", "err", serveErr)
		}
	}()
	return nil
}

// newInterpreter wires the object model over the simulator's state.
func (v *VirtualCCU) newInterpreter() *regavm.Interpreter {
	var rpc = v.xmlrpc
	source := &regaSource{state: v.state}
	if rpc != nil {
		source.rpc = rpc.RPC()
	}
	interfaces := make([]string, 0, len(hmconst.InterfaceOrder))
	interfaces = append(interfaces, hmconst.InterfaceOrder...)
	return &regavm.Interpreter{
		Root: &regaRoot{
			source:     source,
			interfaces: interfaces,
			serial:     v.state.Serial(),
		},
		Exec: v.execCommand,
	}
}

// execCommand answers the shell-outs a script performs.
//
// A simulator has no shell, and running one would be a liability. The
// commands the shipped scripts issue are all queries about the system
// itself, so they are answered from state; anything else reports
// failure, which is what a CCU does for a command that is not there.
func (v *VirtualCCU) execCommand(command string) (string, bool) {
	info := v.state.BackendInfo()
	switch {
	case strings.Contains(command, "hostname"):
		return info.Hostname, true
	case strings.Contains(command, "VERSION") && strings.Contains(command, "PRODUCT"):
		return info.Product, true
	case strings.Contains(command, "VERSION"):
		return info.Version, true
	case strings.Contains(command, "printenv"):
		return "", true
	default:
		return "", false
	}
}

// serveRegaScript runs a posted script and answers with its output.
func serveRegaScript(w http.ResponseWriter, r *http.Request, interpreter *regavm.Interpreter, logger *slog.Logger) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read request", http.StatusBadRequest)
		return
	}
	// A CCU speaks ISO-8859-1 on this endpoint.
	script := binrpc.DecodeLatin1(body)

	result, runErr := interpreter.Run(script)
	if runErr != nil {
		logger.Debug("virtualccu: rega script failed", "err", runErr)
	}

	// The output precedes the XML envelope, which is how a CCU frames
	// it: everything the script wrote, then the trailer.
	w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
	var out strings.Builder
	out.WriteString(result.Output)
	out.WriteString("<xml><exec>/tclrega.exe</exec><sessionId></sessionId><httpUserAgent>")
	out.WriteString(r.UserAgent())
	out.WriteString("</httpUserAgent></xml>")
	_, _ = w.Write(binrpc.EncodeLatin1(out.String()))
}

// RegaScriptAddr returns the bound script endpoint, or nil.
func (v *VirtualCCU) RegaScriptAddr() net.Addr {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.regaScript == nil || v.regaScript.listener == nil {
		return nil
	}
	return v.regaScript.listener.Addr()
}

// stopRegaScript shuts the endpoint down.
func (v *VirtualCCU) stopRegaScript() error {
	server := v.regaScript
	v.regaScript = nil
	if server == nil || server.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.srv.Shutdown(ctx)
}
