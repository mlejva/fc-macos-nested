// Package agent implements the fc-agent that runs inside the Linux VM.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds the agent configuration.
type Config struct {
	HTTPPort         int
	VsockPort        uint32
	FirecrackerBin   string
	SocketPath       string
	SerialSocketPath string
}

// Agent is the fc-agent that proxies requests to Firecracker.
type Agent struct {
	config    *Config
	fcProcess *exec.Cmd
	fcMu      sync.Mutex
	fcStarted bool
	proxy     *httputil.ReverseProxy
	// Console I/O
	consoleIn  io.WriteCloser
	consoleOut io.ReadCloser
}

// New creates a new agent with the given configuration.
func New(cfg *Config) *Agent {
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.SerialSocketPath == "" {
		cfg.SerialSocketPath = "/tmp/firecracker.serial"
	}
	return &Agent{
		config: cfg,
	}
}

// Run starts the agent and listens for HTTP requests.
func (a *Agent) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", a.handleHealth)

	// Agent control endpoints
	mux.HandleFunc("/agent/start", a.handleStartFirecracker)
	mux.HandleFunc("/agent/stop", a.handleStopFirecracker)
	mux.HandleFunc("/agent/status", a.handleStatus)

	// Console streaming endpoint (WebSocket-like via HTTP)
	mux.HandleFunc("/console", a.handleConsole)

	// Proxy all other requests to Firecracker
	mux.HandleFunc("/", a.handleProxy)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.HTTPPort),
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		logrus.Info("Shutting down HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		a.StopFirecracker()
	}()

	logrus.Infof("Agent listening on :%d", a.config.HTTPPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *Agent) handleStartFirecracker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := a.startFirecracker(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "started",
		"pid":    a.fcProcess.Process.Pid,
	})
}

func (a *Agent) handleStopFirecracker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := a.StopFirecracker(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stop Firecracker: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (a *Agent) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.fcMu.Lock()
	defer a.fcMu.Unlock()

	status := map[string]interface{}{
		"firecracker_running": a.fcStarted,
		"socket_path":         a.config.SocketPath,
	}

	if a.fcProcess != nil && a.fcProcess.Process != nil {
		status["pid"] = a.fcProcess.Process.Pid
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (a *Agent) handleConsole(w http.ResponseWriter, r *http.Request) {
	// Start Firecracker if not running
	if !a.fcStarted {
		if err := a.startFirecracker(r.Context()); err != nil {
			http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusServiceUnavailable)
			return
		}
	}

	a.fcMu.Lock()
	consoleIn := a.consoleIn
	consoleOut := a.consoleOut
	a.fcMu.Unlock()

	if consoleIn == nil || consoleOut == nil {
		http.Error(w, "Console not available", http.StatusServiceUnavailable)
		return
	}

	// Upgrade to bidirectional streaming
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Send HTTP 200 OK
	bufrw.WriteString("HTTP/1.1 200 OK\r\n")
	bufrw.WriteString("Content-Type: application/octet-stream\r\n")
	bufrw.WriteString("Connection: close\r\n")
	bufrw.WriteString("\r\n")
	bufrw.Flush()

	logrus.Info("Console connection established")

	// Bidirectional copy
	done := make(chan struct{})

	// Read from console (Firecracker stdout) and write to client
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 1024)
		for {
			n, err := consoleOut.Read(buf)
			if err != nil {
				logrus.Debugf("Console read error: %v", err)
				return
			}
			if n > 0 {
				if _, err := conn.Write(buf[:n]); err != nil {
					logrus.Debugf("Console write to client error: %v", err)
					return
				}
			}
		}
	}()

	// Read from client and write to console (Firecracker stdin)
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				logrus.Debugf("Client read error: %v", err)
				return
			}
			if n > 0 {
				if _, err := consoleIn.Write(buf[:n]); err != nil {
					logrus.Debugf("Console write error: %v", err)
					return
				}
			}
		}
	}()

	// Wait for either direction to complete
	<-done
	logrus.Info("Console connection closed")
}

func (a *Agent) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Start Firecracker if not running
	if !a.fcStarted {
		if err := a.startFirecracker(r.Context()); err != nil {
			logrus.Errorf("Failed to start Firecracker: %v", err)
			http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusServiceUnavailable)
			return
		}
	}

	a.fcMu.Lock()
	proxy := a.proxy
	a.fcMu.Unlock()

	if proxy == nil {
		http.Error(w, "Firecracker not running", http.StatusServiceUnavailable)
		return
	}

	logrus.Debugf("Proxying %s %s", r.Method, r.URL.Path)
	proxy.ServeHTTP(w, r)
}

func (a *Agent) startFirecracker(ctx context.Context) error {
	a.fcMu.Lock()
	defer a.fcMu.Unlock()

	if a.fcStarted {
		return nil
	}

	// Remove old socket if exists
	os.Remove(a.config.SocketPath)

	logrus.Infof("Starting Firecracker: %s", a.config.FirecrackerBin)

	// Check if binary exists
	if _, err := os.Stat(a.config.FirecrackerBin); os.IsNotExist(err) {
		return fmt.Errorf("firecracker binary not found at %s", a.config.FirecrackerBin)
	}

	// Start Firecracker with a background context (not the request context)
	// The request context is cancelled after the HTTP response, which would kill Firecracker
	a.fcProcess = exec.Command(a.config.FirecrackerBin,
		"--api-sock", a.config.SocketPath,
	)

	// Create pipes for console I/O
	var err error
	a.consoleIn, err = a.fcProcess.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	a.consoleOut, err = a.fcProcess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	a.fcProcess.Stderr = os.Stderr

	if err := a.fcProcess.Start(); err != nil {
		return fmt.Errorf("failed to start Firecracker: %w", err)
	}

	// Wait for socket to be available
	if err := a.waitForSocket(30 * time.Second); err != nil {
		a.fcProcess.Process.Kill()
		return err
	}

	// Set up reverse proxy
	a.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost"
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", a.config.SocketPath)
			},
		},
		ModifyResponse: func(resp *http.Response) error {
			logrus.Debugf("Firecracker response: %d", resp.StatusCode)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logrus.Errorf("Proxy error: %v", err)
			http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
		},
	}

	a.fcStarted = true
	logrus.Infof("Firecracker started with PID %d", a.fcProcess.Process.Pid)

	// Monitor Firecracker process
	go func() {
		if err := a.fcProcess.Wait(); err != nil {
			logrus.Errorf("Firecracker exited: %v", err)
		} else {
			logrus.Info("Firecracker exited")
		}
		a.fcMu.Lock()
		a.fcStarted = false
		a.proxy = nil
		a.fcMu.Unlock()
	}()

	return nil
}

func (a *Agent) waitForSocket(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(a.config.SocketPath); err == nil {
			// Try to connect
			conn, err := net.Dial("unix", a.config.SocketPath)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for Firecracker socket")
}

// StopFirecracker stops the Firecracker process.
func (a *Agent) StopFirecracker() error {
	a.fcMu.Lock()
	defer a.fcMu.Unlock()

	if a.fcProcess == nil || !a.fcStarted {
		return nil
	}

	logrus.Info("Stopping Firecracker")

	// Send SIGTERM
	if err := a.fcProcess.Process.Signal(os.Interrupt); err != nil {
		// Fall back to SIGKILL
		return a.fcProcess.Process.Kill()
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		_, err := a.fcProcess.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(5 * time.Second):
		// Timeout, force kill
		a.fcProcess.Process.Kill()
	}

	a.fcStarted = false
	a.fcProcess = nil
	a.proxy = nil

	return nil
}

// Helper to copy files
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
