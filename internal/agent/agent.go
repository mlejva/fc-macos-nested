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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds the agent configuration.
type Config struct {
	HTTPPort       int
	VsockPort      uint32
	FirecrackerBin string
	SocketPath     string // Legacy single-VM socket path
	MaxMicroVMs    int    // Maximum allowed microVMs (default: 10)
}

// MicroVMConfig holds per-microVM configuration.
type MicroVMConfig struct {
	VCPUs     int    `json:"vcpus"`
	MemoryMiB int    `json:"memory_mib"`
	Kernel    string `json:"kernel"`
	Rootfs    string `json:"rootfs"`
	BootArgs  string `json:"boot_args"`
}

// MicroVM represents a single Firecracker microVM instance.
type MicroVM struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	SocketPath string         `json:"socket_path"`
	Config     *MicroVMConfig `json:"config,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`

	fcProcess  *exec.Cmd
	proxy      *httputil.ReverseProxy
	consoleIn  io.WriteCloser
	consoleOut io.ReadCloser
	started    bool
	mu         sync.Mutex
}

// MicroVMInfo is the JSON response for microVM status.
type MicroVMInfo struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Running      bool           `json:"running"`
	PID          int            `json:"pid,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	Config       *MicroVMConfig `json:"config,omitempty"`
	CPUPercent   float64        `json:"cpu_percent,omitempty"`
	MemoryUsedMB int            `json:"memory_used_mb,omitempty"`
}

// CreateMicroVMRequest is the request body for creating a microVM.
type CreateMicroVMRequest struct {
	Name      string `json:"name,omitempty"`
	Kernel    string `json:"kernel"`
	Rootfs    string `json:"rootfs"`
	VCPUs     int    `json:"vcpus"`
	MemoryMiB int    `json:"memory_mib"`
	BootArgs  string `json:"boot_args,omitempty"`
}

// Agent is the fc-agent that proxies requests to Firecracker.
type Agent struct {
	config *Config

	// Multi-VM management
	microVMs  map[string]*MicroVM
	vmMu      sync.RWMutex
	idCounter uint64

	// Legacy single-VM support (for backward compatibility)
	legacyVM *MicroVM
}

// New creates a new agent with the given configuration.
func New(cfg *Config) *Agent {
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.MaxMicroVMs == 0 {
		cfg.MaxMicroVMs = 10
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/tmp/firecracker.socket"
	}
	return &Agent{
		config:   cfg,
		microVMs: make(map[string]*MicroVM),
	}
}

// Run starts the agent and listens for HTTP requests.
func (a *Agent) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", a.handleHealth)

	// Multi-microVM management endpoints
	mux.HandleFunc("/agent/microvms", a.handleMicroVMs)
	mux.HandleFunc("/agent/microvms/", a.handleMicroVMByID)

	// Legacy single-VM endpoints (backward compatibility)
	mux.HandleFunc("/agent/start", a.handleLegacyStart)
	mux.HandleFunc("/agent/stop", a.handleLegacyStop)
	mux.HandleFunc("/agent/status", a.handleLegacyStatus)
	mux.HandleFunc("/console", a.handleLegacyConsole)

	// Proxy to Firecracker (handles both legacy and multi-VM)
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
		a.stopAllMicroVMs()
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

// ==================== Multi-MicroVM Handlers ====================

func (a *Agent) handleMicroVMs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listMicroVMs(w, r)
	case http.MethodPost:
		a.createMicroVM(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *Agent) listMicroVMs(w http.ResponseWriter, r *http.Request) {
	a.vmMu.RLock()
	defer a.vmMu.RUnlock()

	vms := make([]MicroVMInfo, 0, len(a.microVMs))
	for _, vm := range a.microVMs {
		vm.mu.Lock()
		info := MicroVMInfo{
			ID:        vm.ID,
			Name:      vm.Name,
			Running:   vm.started,
			CreatedAt: vm.CreatedAt,
			Config:    vm.Config,
		}
		if vm.fcProcess != nil && vm.fcProcess.Process != nil {
			info.PID = vm.fcProcess.Process.Pid
			// Get resource usage
			cpu, mem := getProcessStats(info.PID)
			info.CPUPercent = cpu
			info.MemoryUsedMB = mem
		}
		vm.mu.Unlock()
		vms = append(vms, info)
	}

	// Sort by name for stable ordering
	sort.Slice(vms, func(i, j int) bool {
		return vms[i].Name < vms[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vms)
}

// getProcessStats returns CPU percentage and memory usage in MB for a process
// Uses ps command for accurate real-time stats
func getProcessStats(pid int) (cpuPercent float64, memoryMB int) {
	// Use ps to get real-time CPU and memory usage
	// %cpu = CPU percentage, rss = resident set size in KB
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	fields := strings.Fields(string(output))
	if len(fields) >= 2 {
		cpuPercent, _ = strconv.ParseFloat(fields[0], 64)
		if rssKB, err := strconv.Atoi(fields[1]); err == nil {
			memoryMB = rssKB / 1024
		}
	}

	return cpuPercent, memoryMB
}

func (a *Agent) createMicroVM(w http.ResponseWriter, r *http.Request) {
	var req CreateMicroVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Kernel == "" {
		http.Error(w, "kernel is required", http.StatusBadRequest)
		return
	}
	if req.Rootfs == "" {
		http.Error(w, "rootfs is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.VCPUs == 0 {
		req.VCPUs = 1
	}
	if req.MemoryMiB == 0 {
		req.MemoryMiB = 128
	}
	if req.BootArgs == "" {
		req.BootArgs = "console=ttyS0 reboot=k panic=1 pci=off"
	}

	// Check max VMs limit
	a.vmMu.RLock()
	if len(a.microVMs) >= a.config.MaxMicroVMs {
		a.vmMu.RUnlock()
		http.Error(w, fmt.Sprintf("Maximum microVMs limit reached (%d)", a.config.MaxMicroVMs), http.StatusTooManyRequests)
		return
	}
	a.vmMu.RUnlock()

	// Generate ID and name
	id := a.generateID()
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("microvm-%d", atomic.LoadUint64(&a.idCounter))
	}

	// Check for name collision
	a.vmMu.RLock()
	for _, existing := range a.microVMs {
		if existing.Name == name {
			a.vmMu.RUnlock()
			http.Error(w, fmt.Sprintf("microVM with name '%s' already exists", name), http.StatusConflict)
			return
		}
	}
	a.vmMu.RUnlock()

	// Create socket path
	socketPath := fmt.Sprintf("/tmp/firecracker-%s.socket", id)

	vm := &MicroVM{
		ID:         id,
		Name:       name,
		SocketPath: socketPath,
		CreatedAt:  time.Now(),
		Config: &MicroVMConfig{
			VCPUs:     req.VCPUs,
			MemoryMiB: req.MemoryMiB,
			Kernel:    req.Kernel,
			Rootfs:    req.Rootfs,
			BootArgs:  req.BootArgs,
		},
	}

	// Start Firecracker process
	if err := a.startFirecrackerForVM(vm); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusInternalServerError)
		return
	}

	// Configure and start the microVM
	if err := a.configureAndStartVM(r.Context(), vm); err != nil {
		a.stopFirecrackerForVM(vm)
		http.Error(w, fmt.Sprintf("Failed to configure microVM: %v", err), http.StatusInternalServerError)
		return
	}

	// Register the VM
	a.vmMu.Lock()
	a.microVMs[id] = vm
	a.vmMu.Unlock()

	logrus.Infof("Created microVM: %s (%s)", vm.Name, vm.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(MicroVMInfo{
		ID:        vm.ID,
		Name:      vm.Name,
		Running:   vm.started,
		PID:       vm.fcProcess.Process.Pid,
		CreatedAt: vm.CreatedAt,
		Config:    vm.Config,
	})
}

func (a *Agent) handleMicroVMByID(w http.ResponseWriter, r *http.Request) {
	// Parse path: /agent/microvms/{id} or /agent/microvms/{id}/console
	path := strings.TrimPrefix(r.URL.Path, "/agent/microvms/")
	parts := strings.SplitN(path, "/", 2)
	vmID := parts[0]

	if vmID == "" {
		http.Error(w, "microVM ID required", http.StatusBadRequest)
		return
	}

	// Get VM by ID or name
	vm := a.getVMByIDOrName(vmID)
	if vm == nil {
		http.Error(w, fmt.Sprintf("microVM not found: %s", vmID), http.StatusNotFound)
		return
	}

	// Check for sub-path
	if len(parts) > 1 {
		switch parts[1] {
		case "console":
			a.handleVMConsole(w, r, vm)
			return
		default:
			// Proxy to Firecracker API for this VM
			a.proxyToVM(w, r, vm, "/"+parts[1])
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		a.getMicroVMStatus(w, vm)
	case http.MethodDelete:
		a.deleteMicroVM(w, r, vm)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *Agent) getMicroVMStatus(w http.ResponseWriter, vm *MicroVM) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	info := MicroVMInfo{
		ID:        vm.ID,
		Name:      vm.Name,
		Running:   vm.started,
		CreatedAt: vm.CreatedAt,
		Config:    vm.Config,
	}
	if vm.fcProcess != nil && vm.fcProcess.Process != nil {
		info.PID = vm.fcProcess.Process.Pid
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (a *Agent) deleteMicroVM(w http.ResponseWriter, r *http.Request, vm *MicroVM) {
	force := r.URL.Query().Get("force") == "true"

	if err := a.stopFirecrackerForVM(vm); err != nil && !force {
		http.Error(w, fmt.Sprintf("Failed to stop microVM: %v", err), http.StatusInternalServerError)
		return
	}

	// Remove from registry
	a.vmMu.Lock()
	delete(a.microVMs, vm.ID)
	a.vmMu.Unlock()

	logrus.Infof("Deleted microVM: %s (%s)", vm.Name, vm.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": vm.ID})
}

func (a *Agent) handleVMConsole(w http.ResponseWriter, r *http.Request, vm *MicroVM) {
	vm.mu.Lock()
	if !vm.started {
		vm.mu.Unlock()
		http.Error(w, "microVM not running", http.StatusServiceUnavailable)
		return
	}
	consoleIn := vm.consoleIn
	consoleOut := vm.consoleOut
	vm.mu.Unlock()

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

	logrus.Infof("Console connection established for %s", vm.Name)

	// Bidirectional copy
	done := make(chan struct{})

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 1024)
		for {
			n, err := consoleOut.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				if _, err := conn.Write(buf[:n]); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				if _, err := consoleIn.Write(buf[:n]); err != nil {
					return
				}
			}
		}
	}()

	<-done
	logrus.Infof("Console connection closed for %s", vm.Name)
}

// ==================== Legacy Single-VM Handlers ====================

func (a *Agent) handleLegacyStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Create or get legacy VM
	if a.legacyVM == nil {
		a.legacyVM = &MicroVM{
			ID:         "legacy",
			Name:       "default",
			SocketPath: a.config.SocketPath,
			CreatedAt:  time.Now(),
		}
	}

	if a.legacyVM.started {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "already_running",
			"pid":    a.legacyVM.fcProcess.Process.Pid,
		})
		return
	}

	if err := a.startFirecrackerForVM(a.legacyVM); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "started",
		"pid":    a.legacyVM.fcProcess.Process.Pid,
	})
}

func (a *Agent) handleLegacyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.legacyVM == nil || !a.legacyVM.started {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "not_running"})
		return
	}

	if err := a.stopFirecrackerForVM(a.legacyVM); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stop Firecracker: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (a *Agent) handleLegacyStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"firecracker_running": false,
		"socket_path":         a.config.SocketPath,
	}

	if a.legacyVM != nil {
		a.legacyVM.mu.Lock()
		status["firecracker_running"] = a.legacyVM.started
		if a.legacyVM.fcProcess != nil && a.legacyVM.fcProcess.Process != nil {
			status["pid"] = a.legacyVM.fcProcess.Process.Pid
		}
		a.legacyVM.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (a *Agent) handleLegacyConsole(w http.ResponseWriter, r *http.Request) {
	if a.legacyVM == nil {
		// Auto-create legacy VM
		a.legacyVM = &MicroVM{
			ID:         "legacy",
			Name:       "default",
			SocketPath: a.config.SocketPath,
			CreatedAt:  time.Now(),
		}
	}

	if !a.legacyVM.started {
		if err := a.startFirecrackerForVM(a.legacyVM); err != nil {
			http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusServiceUnavailable)
			return
		}
	}

	a.handleVMConsole(w, r, a.legacyVM)
}

// ==================== Proxy Handler ====================

func (a *Agent) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Check if this is a multi-VM proxy request: /microvms/{id}/path
	if strings.HasPrefix(r.URL.Path, "/microvms/") {
		path := strings.TrimPrefix(r.URL.Path, "/microvms/")
		parts := strings.SplitN(path, "/", 2)
		vmID := parts[0]

		vm := a.getVMByIDOrName(vmID)
		if vm == nil {
			http.Error(w, fmt.Sprintf("microVM not found: %s", vmID), http.StatusNotFound)
			return
		}

		apiPath := "/"
		if len(parts) > 1 {
			apiPath = "/" + parts[1]
		}
		a.proxyToVM(w, r, vm, apiPath)
		return
	}

	// Check for X-MicroVM-ID header
	if vmID := r.Header.Get("X-MicroVM-ID"); vmID != "" {
		vm := a.getVMByIDOrName(vmID)
		if vm == nil {
			http.Error(w, fmt.Sprintf("microVM not found: %s", vmID), http.StatusNotFound)
			return
		}
		a.proxyToVM(w, r, vm, r.URL.Path)
		return
	}

	// Legacy behavior: use legacy VM or first available
	vm := a.legacyVM
	if vm == nil {
		// Try to get first available VM
		a.vmMu.RLock()
		for _, v := range a.microVMs {
			vm = v
			break
		}
		a.vmMu.RUnlock()
	}

	if vm == nil {
		// Auto-create legacy VM for backward compatibility
		a.legacyVM = &MicroVM{
			ID:         "legacy",
			Name:       "default",
			SocketPath: a.config.SocketPath,
			CreatedAt:  time.Now(),
		}
		vm = a.legacyVM
	}

	if !vm.started {
		if err := a.startFirecrackerForVM(vm); err != nil {
			http.Error(w, fmt.Sprintf("Failed to start Firecracker: %v", err), http.StatusServiceUnavailable)
			return
		}
	}

	a.proxyToVM(w, r, vm, r.URL.Path)
}

func (a *Agent) proxyToVM(w http.ResponseWriter, r *http.Request, vm *MicroVM, path string) {
	vm.mu.Lock()
	proxy := vm.proxy
	vm.mu.Unlock()

	if proxy == nil {
		http.Error(w, "Firecracker not running", http.StatusServiceUnavailable)
		return
	}

	// Update request path
	r.URL.Path = path

	logrus.Debugf("Proxying %s %s to %s", r.Method, path, vm.Name)
	proxy.ServeHTTP(w, r)
}

// ==================== Firecracker Management ====================

func (a *Agent) generateID() string {
	counter := atomic.AddUint64(&a.idCounter, 1)
	return fmt.Sprintf("vm-%d-%d", time.Now().Unix(), counter)
}

func (a *Agent) getVMByIDOrName(idOrName string) *MicroVM {
	a.vmMu.RLock()
	defer a.vmMu.RUnlock()

	// Check by exact ID
	if vm, ok := a.microVMs[idOrName]; ok {
		return vm
	}

	// Check by name or ID prefix
	for id, vm := range a.microVMs {
		if vm.Name == idOrName || strings.HasPrefix(id, idOrName) {
			return vm
		}
	}

	// Check legacy VM
	if a.legacyVM != nil && (a.legacyVM.ID == idOrName || a.legacyVM.Name == idOrName) {
		return a.legacyVM
	}

	return nil
}

func (a *Agent) startFirecrackerForVM(vm *MicroVM) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.started {
		return nil
	}

	// Remove old socket if exists
	os.Remove(vm.SocketPath)

	logrus.Infof("Starting Firecracker for %s: %s", vm.Name, a.config.FirecrackerBin)

	// Check if binary exists
	if _, err := os.Stat(a.config.FirecrackerBin); os.IsNotExist(err) {
		return fmt.Errorf("firecracker binary not found at %s", a.config.FirecrackerBin)
	}

	vm.fcProcess = exec.Command(a.config.FirecrackerBin,
		"--api-sock", vm.SocketPath,
		"--level", "Warning",
	)

	// Create pipes for console I/O
	var err error
	vm.consoleIn, err = vm.fcProcess.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	vm.consoleOut, err = vm.fcProcess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	vm.fcProcess.Stderr = os.Stderr

	if err := vm.fcProcess.Start(); err != nil {
		return fmt.Errorf("failed to start Firecracker: %w", err)
	}

	// Wait for socket
	if err := a.waitForSocketPath(vm.SocketPath, 30*time.Second); err != nil {
		vm.fcProcess.Process.Kill()
		return err
	}

	// Set up reverse proxy
	vm.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost"
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", vm.SocketPath)
			},
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logrus.Errorf("Proxy error for %s: %v", vm.Name, err)
			http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
		},
	}

	vm.started = true
	logrus.Infof("Firecracker started for %s with PID %d", vm.Name, vm.fcProcess.Process.Pid)

	// Monitor process
	go func() {
		if err := vm.fcProcess.Wait(); err != nil {
			logrus.Errorf("Firecracker exited for %s: %v", vm.Name, err)
		} else {
			logrus.Infof("Firecracker exited for %s", vm.Name)
		}
		vm.mu.Lock()
		vm.started = false
		vm.proxy = nil
		vm.mu.Unlock()
	}()

	return nil
}

func (a *Agent) configureAndStartVM(ctx context.Context, vm *MicroVM) error {
	if vm.Config == nil {
		return fmt.Errorf("no configuration provided")
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", vm.SocketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	// Configure boot source
	bootSource := map[string]interface{}{
		"kernel_image_path": vm.Config.Kernel,
		"boot_args":         vm.Config.BootArgs,
	}
	if err := a.putJSON(client, "http://localhost/boot-source", bootSource); err != nil {
		return fmt.Errorf("failed to set boot source: %w", err)
	}

	// Configure rootfs drive
	drive := map[string]interface{}{
		"drive_id":       "rootfs",
		"path_on_host":   vm.Config.Rootfs,
		"is_root_device": true,
		"is_read_only":   false,
	}
	if err := a.putJSON(client, "http://localhost/drives/rootfs", drive); err != nil {
		return fmt.Errorf("failed to set rootfs: %w", err)
	}

	// Configure machine
	machine := map[string]interface{}{
		"vcpu_count":  vm.Config.VCPUs,
		"mem_size_mib": vm.Config.MemoryMiB,
	}
	if err := a.putJSON(client, "http://localhost/machine-config", machine); err != nil {
		return fmt.Errorf("failed to set machine config: %w", err)
	}

	// Start the instance
	action := map[string]interface{}{
		"action_type": "InstanceStart",
	}
	if err := a.putJSON(client, "http://localhost/actions", action); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	return nil
}

func (a *Agent) putJSON(client *http.Client, url string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (a *Agent) stopFirecrackerForVM(vm *MicroVM) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.fcProcess == nil || !vm.started {
		return nil
	}

	logrus.Infof("Stopping Firecracker for %s", vm.Name)

	// Send SIGTERM
	if err := vm.fcProcess.Process.Signal(os.Interrupt); err != nil {
		return vm.fcProcess.Process.Kill()
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		_, err := vm.fcProcess.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		vm.fcProcess.Process.Kill()
	}

	vm.started = false
	vm.fcProcess = nil
	vm.proxy = nil

	// Clean up socket
	os.Remove(vm.SocketPath)

	return nil
}

func (a *Agent) waitForSocketPath(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			conn, err := net.Dial("unix", socketPath)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for Firecracker socket")
}

func (a *Agent) stopAllMicroVMs() {
	a.vmMu.Lock()
	defer a.vmMu.Unlock()

	for _, vm := range a.microVMs {
		a.stopFirecrackerForVM(vm)
	}

	if a.legacyVM != nil {
		a.stopFirecrackerForVM(a.legacyVM)
	}
}

// StopFirecracker stops the legacy Firecracker process (backward compatibility).
func (a *Agent) StopFirecracker() error {
	if a.legacyVM != nil {
		return a.stopFirecrackerForVM(a.legacyVM)
	}
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
