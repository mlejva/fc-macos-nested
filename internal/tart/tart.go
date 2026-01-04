// Package tart provides a wrapper around the Tart CLI for managing Linux VMs.
// Tart is used as the VM layer to avoid macOS Tahoe signing requirements,
// since Tart is properly signed and notarized by Cirrus Labs.
package tart

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultTartPath is the default location for the Tart binary
	DefaultTartPath = "tart"

	// VMName is the name used for our Linux VM
	VMName = "fc-macos-linux"

	// DefaultSSHPort is the default SSH port inside the VM
	DefaultSSHPort = 22
)

// Config holds configuration for the Tart VM
type Config struct {
	TartPath      string
	VMName        string
	CPUCount      uint
	MemorySizeMiB uint64
	DiskSizeGB    uint
	SharedDirs    []SharedDir
	Nested        bool
}

// SharedDir represents a directory share configuration
type SharedDir struct {
	Name     string
	HostPath string
	ReadOnly bool
}

// VM represents a Tart-managed Linux VM
type VM struct {
	config    *Config
	cmd       *exec.Cmd
	state     string
	stateMu   sync.RWMutex
	sshClient *SSHClient
	cancel    context.CancelFunc
	logs      *logBuffer
}

// logBuffer is a thread-safe buffer for VM logs
type logBuffer struct {
	mu    sync.RWMutex
	lines []string
}

func (l *logBuffer) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if line != "" {
			l.lines = append(l.lines, line)
			// Keep only last 1000 lines
			if len(l.lines) > 1000 {
				l.lines = l.lines[1:]
			}
		}
	}
	return len(p), nil
}

func (l *logBuffer) Lines(n int) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	start := len(l.lines) - n
	if start < 0 {
		start = 0
	}
	result := make([]string, len(l.lines)-start)
	copy(result, l.lines[start:])
	return result
}

// VMInfo represents VM information from tart get
type VMInfo struct {
	CPU     int    `json:"cpu"`
	Memory  int    `json:"memory"`
	Disk    int    `json:"disk"`
	Display string `json:"display"`
	State   string `json:"state"`
	Running bool   `json:"running"`
	OS      string `json:"os"`
}

// New creates a new Tart VM instance
func New(cfg *Config) (*VM, error) {
	if cfg.TartPath == "" {
		cfg.TartPath = findTartBinary()
	}

	if cfg.VMName == "" {
		cfg.VMName = VMName
	}

	if cfg.CPUCount == 0 {
		cfg.CPUCount = 2
	}

	if cfg.MemorySizeMiB == 0 {
		cfg.MemorySizeMiB = 2048
	}

	if cfg.DiskSizeGB == 0 {
		cfg.DiskSizeGB = 20
	}

	// Verify tart binary exists
	if _, err := exec.LookPath(cfg.TartPath); err != nil {
		return nil, fmt.Errorf("tart binary not found at %s: %w", cfg.TartPath, err)
	}

	return &VM{
		config: cfg,
		state:  "stopped",
		logs:   &logBuffer{lines: make([]string, 0)},
	}, nil
}

// findTartBinary looks for the tart binary in common locations
func findTartBinary() string {
	// Check common locations
	paths := []string{
		"tart",
		"/usr/local/bin/tart",
		"/opt/homebrew/bin/tart",
		filepath.Join(os.Getenv("HOME"), "Applications/tart.app/Contents/MacOS/tart"),
		"/Applications/tart.app/Contents/MacOS/tart",
	}

	for _, p := range paths {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}

	return "tart"
}

// EnsureVM ensures the Linux VM exists, creating it if necessary
func (v *VM) EnsureVM(ctx context.Context, linuxImage string) error {
	// Check if VM exists
	exists, err := v.vmExists(ctx)
	if err != nil {
		return err
	}

	if !exists {
		logrus.Infof("Creating Linux VM from image: %s", linuxImage)
		if err := v.createVM(ctx, linuxImage); err != nil {
			return fmt.Errorf("failed to create VM: %w", err)
		}
	}

	// Configure VM
	if err := v.configureVM(ctx); err != nil {
		return fmt.Errorf("failed to configure VM: %w", err)
	}

	return nil
}

// vmExists checks if the VM already exists
func (v *VM) vmExists(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, v.config.TartPath, "list", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return false, nil // List might fail if no VMs exist
	}

	var vms []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &vms); err != nil {
		return false, nil
	}

	for _, vm := range vms {
		if vm.Name == v.config.VMName {
			return true, nil
		}
	}

	return false, nil
}

// createVM creates a new VM from a Linux image
func (v *VM) createVM(ctx context.Context, linuxImage string) error {
	// Clone from the provided image
	cmd := exec.CommandContext(ctx, v.config.TartPath, "clone", linuxImage, v.config.VMName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// configureVM configures the VM with specified resources
func (v *VM) configureVM(ctx context.Context) error {
	args := []string{
		"set", v.config.VMName,
		"--cpu", fmt.Sprintf("%d", v.config.CPUCount),
		"--memory", fmt.Sprintf("%d", v.config.MemorySizeMiB),
	}

	cmd := exec.CommandContext(ctx, v.config.TartPath, args...)
	return cmd.Run()
}

// Start starts the VM
func (v *VM) Start(ctx context.Context) error {
	v.stateMu.Lock()
	if v.state == "running" {
		v.stateMu.Unlock()
		return nil
	}
	v.stateMu.Unlock()

	// Create a cancellable context for the VM process
	vmCtx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel

	args := []string{"run", v.config.VMName, "--no-graphics"}

	if v.config.Nested {
		args = append(args, "--nested")
	}

	// Add shared directories
	for _, dir := range v.config.SharedDirs {
		dirArg := dir.HostPath
		if dir.Name != "" {
			dirArg = fmt.Sprintf("%s:%s", dir.Name, dir.HostPath)
		}
		if dir.ReadOnly {
			dirArg += ":ro"
		}
		args = append(args, "--dir", dirArg)
	}

	logrus.Infof("Starting VM with: %s %v", v.config.TartPath, args)

	v.cmd = exec.CommandContext(vmCtx, v.config.TartPath, args...)
	v.cmd.Stdout = v.logs
	v.cmd.Stderr = v.logs

	if err := v.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	v.stateMu.Lock()
	v.state = "starting"
	v.stateMu.Unlock()

	// Start a goroutine to monitor the process
	go func() {
		err := v.cmd.Wait()
		v.stateMu.Lock()
		if err != nil && v.state != "stopping" {
			logrus.Errorf("VM process exited with error: %v", err)
		}
		v.state = "stopped"
		v.stateMu.Unlock()
	}()

	// Wait for VM to be ready
	if err := v.waitForReady(ctx); err != nil {
		v.Stop(context.Background(), true)
		return fmt.Errorf("VM failed to become ready: %w", err)
	}

	v.stateMu.Lock()
	v.state = "running"
	v.stateMu.Unlock()

	return nil
}

// waitForReady waits for the VM to be ready (have an IP address)
func (v *VM) waitForReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for VM to be ready")
		case <-ticker.C:
			ip, err := v.GetIP(ctx)
			if err == nil && ip != "" {
				logrus.Infof("VM is ready with IP: %s", ip)
				return nil
			}
		}
	}
}

// Stop stops the VM
func (v *VM) Stop(ctx context.Context, force bool) error {
	v.stateMu.Lock()
	if v.state == "stopped" {
		v.stateMu.Unlock()
		return nil
	}
	v.state = "stopping"
	v.stateMu.Unlock()

	// Cancel the VM context
	if v.cancel != nil {
		v.cancel()
	}

	// Use tart stop command
	args := []string{"stop", v.config.VMName}
	if force {
		// Tart doesn't have a force flag, but stopping should work
	}

	cmd := exec.CommandContext(ctx, v.config.TartPath, args...)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("tart stop failed: %v", err)
	}

	// Wait for process to exit
	if v.cmd != nil && v.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			v.cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			v.cmd.Process.Kill()
		}
	}

	v.stateMu.Lock()
	v.state = "stopped"
	v.stateMu.Unlock()

	return nil
}

// State returns the current VM state
func (v *VM) State() string {
	v.stateMu.RLock()
	defer v.stateMu.RUnlock()
	return v.state
}

// GetIP returns the VM's IP address
func (v *VM) GetIP(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, v.config.TartPath, "ip", v.config.VMName)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetInfo returns VM information
func (v *VM) GetInfo(ctx context.Context) (*VMInfo, error) {
	cmd := exec.CommandContext(ctx, v.config.TartPath, "get", v.config.VMName, "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var info VMInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// Exec executes a command in the VM via SSH
func (v *VM) Exec(ctx context.Context, command string) (string, error) {
	ip, err := v.GetIP(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get VM IP: %w", err)
	}

	// Use tart exec if available, otherwise fall back to SSH
	cmd := exec.CommandContext(ctx, v.config.TartPath, "exec", v.config.VMName, "--", "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try SSH as fallback
		return v.execSSH(ctx, ip, command)
	}

	return string(output), nil
}

// execSSH executes a command via SSH
func (v *VM) execSSH(ctx context.Context, ip, command string) (string, error) {
	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", ip),
		command,
	)
	output, err := sshCmd.CombinedOutput()
	return string(output), err
}

// Logs returns the last n lines of VM logs
func (v *VM) Logs(ctx context.Context, follow bool, lines int) error {
	logLines := v.logs.Lines(lines)
	for _, line := range logLines {
		fmt.Println(line)
	}

	if follow {
		// For follow mode, we'd need to implement a proper log streaming
		// For now, just return the existing logs
		return fmt.Errorf("follow mode not fully implemented for Tart VMs")
	}

	return nil
}

// Shell opens an interactive shell to the VM
func (v *VM) Shell(ctx context.Context) error {
	ip, err := v.GetIP(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM IP: %w", err)
	}

	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", ip),
	)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return sshCmd.Run()
}

// PingAgent checks if the fc-agent is responding in the VM
func (v *VM) PingAgent() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := v.Exec(ctx, "curl -s http://localhost:8080/health || echo 'failed'")
	if err != nil {
		return false
	}

	return strings.Contains(output, "ok") || strings.Contains(output, "healthy")
}

// Config returns the VM configuration
func (v *VM) Config() *Config {
	return v.config
}

// Delete deletes the VM
func (v *VM) Delete(ctx context.Context) error {
	// Stop first if running
	v.Stop(ctx, true)

	cmd := exec.CommandContext(ctx, v.config.TartPath, "delete", v.config.VMName)
	return cmd.Run()
}

// SSHClient is a placeholder for SSH client functionality
type SSHClient struct {
	host string
	port int
}

// StreamLogs streams logs from the VM
func (v *VM) StreamLogs(ctx context.Context, w io.Writer) error {
	// Start streaming from where we are
	lastIndex := 0

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			lines := v.logs.Lines(0)
			for i := lastIndex; i < len(lines); i++ {
				fmt.Fprintln(w, lines[i])
			}
			lastIndex = len(lines)
		}
	}
}

// CopyFile copies a file to the VM via SSH
func (v *VM) CopyFile(ctx context.Context, localPath, remotePath string) error {
	ip, err := v.GetIP(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM IP: %w", err)
	}

	scpCmd := exec.CommandContext(ctx, "scp",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		localPath,
		fmt.Sprintf("root@%s:%s", ip, remotePath),
	)
	return scpCmd.Run()
}

// ReadFile reads a file from the VM via SSH
func (v *VM) ReadFile(ctx context.Context, remotePath string) ([]byte, error) {
	ip, err := v.GetIP(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM IP: %w", err)
	}

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", ip),
		fmt.Sprintf("cat %s", remotePath),
	)
	return sshCmd.Output()
}

// WaitForSSH waits until SSH is available
func (v *VM) WaitForSSH(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for SSH")
		case <-ticker.C:
			ip, err := v.GetIP(ctx)
			if err != nil {
				continue
			}

			_, err = v.execSSH(ctx, ip, "echo ok")
			if err == nil {
				return nil
			}
		}
	}
}

// ListenSerial opens the serial console for the VM
func (v *VM) ListenSerial(ctx context.Context) (*bufio.Reader, error) {
	// Tart's serial is accessed via --serial flag during run
	// For an already running VM, we'd need to access it differently
	return nil, fmt.Errorf("serial console access requires restarting VM with --serial flag")
}
