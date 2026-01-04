package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// MicroVMInfo matches the agent's response structure
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

type MicroVMConfig struct {
	VCPUs     int    `json:"vcpus"`
	MemoryMiB int    `json:"memory_mib"`
	Kernel    string `json:"kernel"`
	Rootfs    string `json:"rootfs"`
	BootArgs  string `json:"boot_args"`
}

func newRunCmd() *cobra.Command {
	var (
		name       string
		vcpus      int
		memoryMiB  int
		kernel     string
		rootfs     string
		bootArgs   string
		background bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start a Firecracker microVM",
		Long: `Start a Firecracker microVM with the specified configuration.

This command will:
1. Ensure the intermediate Linux VM is running
2. Create and configure a new microVM
3. Start the microVM
4. Connect to the console (unless --background is specified)`,
		Example: `  # Start with auto-generated name
  fc-macos run

  # Start with custom name
  fc-macos run --name my-vm

  # Start with custom configuration
  fc-macos run --name web-server --vcpus 2 --memory 512

  # Start in background
  fc-macos run --name worker-1 --background`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMicroVM(cmd.Context(), name, vcpus, memoryMiB, kernel, rootfs, bootArgs, background)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "name for the microVM (auto-generated if not provided)")
	cmd.Flags().IntVar(&vcpus, "vcpus", 1, "number of vCPUs for the microVM")
	cmd.Flags().IntVar(&memoryMiB, "memory", 128, "memory in MiB for the microVM")
	cmd.Flags().StringVar(&kernel, "kernel", "/var/lib/firecracker/kernels/vmlinux", "path to kernel inside the VM")
	cmd.Flags().StringVar(&rootfs, "rootfs", "/var/lib/firecracker/rootfs/alpine-shell.ext4", "path to rootfs inside the VM")
	cmd.Flags().StringVar(&bootArgs, "boot-args", "console=ttyS0 reboot=k panic=1 pci=off init=/init", "kernel boot arguments")
	cmd.Flags().BoolVar(&background, "background", false, "run in background")

	return cmd
}

func runMicroVM(ctx context.Context, name string, vcpus, memoryMiB int, kernel, rootfs, bootArgs string, background bool) error {
	tartPath := findTart()
	if tartPath == "" {
		return fmt.Errorf("tart not found")
	}

	vmName := "fc-macos-linux"

	// Check if VM is running
	if !isVMRunning(ctx, tartPath, vmName) {
		return fmt.Errorf("VM is not running. Run 'fc-macos setup' first")
	}

	// Get VM IP
	ipCmd := exec.CommandContext(ctx, tartPath, "ip", vmName)
	output, err := ipCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get VM IP: %w", err)
	}
	vmIP := strings.TrimSpace(string(output))

	logrus.Infof("Connecting to fc-agent at %s:8080", vmIP)

	// Wait for agent to be ready
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)
	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < 10; i++ {
		resp, err := client.Get(agentURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if i == 9 {
			return fmt.Errorf("fc-agent not responding at %s", agentURL)
		}
		time.Sleep(time.Second)
	}

	// Create microVM via new API
	logrus.Info("Creating microVM...")

	createReq := map[string]interface{}{
		"name":       name, // Empty string means auto-generate
		"kernel":     kernel,
		"rootfs":     rootfs,
		"vcpus":      vcpus,
		"memory_mib": memoryMiB,
		"boot_args":  bootArgs,
	}

	reqBody, err := json.Marshal(createReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", agentURL+"/agent/microvms", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create microVM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create microVM: %s", string(body))
	}

	var vmInfo MicroVMInfo
	if err := json.NewDecoder(resp.Body).Decode(&vmInfo); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Println()
	fmt.Println("=== MicroVM Started ===")
	fmt.Printf("ID:     %s\n", vmInfo.ID)
	fmt.Printf("Name:   %s\n", vmInfo.Name)
	fmt.Printf("vCPUs:  %d\n", vmInfo.Config.VCPUs)
	fmt.Printf("Memory: %d MiB\n", vmInfo.Config.MemoryMiB)
	fmt.Printf("Kernel: %s\n", vmInfo.Config.Kernel)
	fmt.Printf("Rootfs: %s\n", vmInfo.Config.Rootfs)
	fmt.Println()

	if background {
		fmt.Println("Commands:")
		fmt.Printf("  fc-macos microvm status --name %s\n", vmInfo.Name)
		fmt.Printf("  fc-macos microvm shell --name %s\n", vmInfo.Name)
		fmt.Printf("  fc-macos microvm stop --name %s\n", vmInfo.Name)
		fmt.Println()
		fmt.Println("  fc-macos microvm list              # List all microVMs")
		fmt.Println("  fc-macos dashboard                 # Open live dashboard")
		fmt.Println()
		logrus.Info("Running in background")
		return nil
	}

	// Connect to serial console for this specific VM
	fmt.Println("Connecting to serial console...")
	fmt.Println("Press Ctrl+] to exit")
	fmt.Println()

	return connectToVMConsole(ctx, agentURL, vmInfo.ID)
}

// connectToVMConsole connects to a specific microVM's console
func connectToVMConsole(ctx context.Context, agentURL string, vmID string) error {
	// Connect to the console endpoint for this VM
	conn, err := net.Dial("tcp", strings.TrimPrefix(agentURL, "http://"))
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	// Send HTTP request for VM-specific console
	fmt.Fprintf(conn, "GET /agent/microvms/%s/console HTTP/1.1\r\nHost: localhost\r\n\r\n", vmID)

	// Read HTTP response header
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	// Skip the HTTP header (we expect 200 OK)
	if !strings.Contains(string(buf[:n]), "200 OK") {
		return fmt.Errorf("console not available: %s", string(buf[:n]))
	}

	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		logrus.Warnf("Could not set raw mode: %v", err)
	} else {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Handle Ctrl+C and Ctrl+]
	done := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Copy from console to stdout
	go func() {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			os.Stdout.Write(buf[:n])
		}
	}()

	// Copy from stdin to console, watching for Ctrl+]
	go func() {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				// Check for Ctrl+] (ASCII 29)
				if buf[0] == 29 {
					fmt.Println("\r\nDisconnected from console")
					return
				}
				conn.Write(buf[:n])
			}
		}
	}()

	// Wait for disconnect or signal
	select {
	case <-done:
	case <-sigCh:
		fmt.Println("\r\nReceived signal, disconnecting...")
	case <-ctx.Done():
	}

	return nil
}

// Legacy console connection (for backward compatibility)
func connectToConsole(ctx context.Context, agentURL string) error {
	// Connect to the legacy console endpoint
	conn, err := net.Dial("tcp", strings.TrimPrefix(agentURL, "http://"))
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	// Send HTTP request for console
	fmt.Fprintf(conn, "GET /console HTTP/1.1\r\nHost: localhost\r\n\r\n")

	// Read HTTP response header
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	// Skip the HTTP header (we expect 200 OK)
	if !strings.Contains(string(buf[:n]), "200 OK") {
		return fmt.Errorf("console not available: %s", string(buf[:n]))
	}

	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		logrus.Warnf("Could not set raw mode: %v", err)
	} else {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Handle Ctrl+C and Ctrl+]
	done := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Copy from console to stdout
	go func() {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			os.Stdout.Write(buf[:n])
		}
	}()

	// Copy from stdin to console, watching for Ctrl+]
	go func() {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				// Check for Ctrl+] (ASCII 29)
				if buf[0] == 29 {
					fmt.Println("\r\nDisconnected from console")
					return
				}
				conn.Write(buf[:n])
			}
		}
	}()

	// Wait for disconnect or signal
	select {
	case <-done:
	case <-sigCh:
		fmt.Println("\r\nReceived signal, disconnecting...")
	case <-ctx.Done():
	}

	return nil
}

func doAgentRequest(ctx context.Context, client *http.Client, baseURL, method, path, body string) error {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, strings.NewReader(body))
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
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	return nil
}
