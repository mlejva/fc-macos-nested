package cli

import (
	"context"
	"encoding/json"
	"fmt"
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

func newRunCmd() *cobra.Command {
	var (
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
2. Configure the microVM (kernel, rootfs, machine config)
3. Start the microVM
4. Wait for the microVM to exit (unless --background is specified)`,
		Example: `  # Start with defaults
  fc-macos run

  # Start with custom configuration
  fc-macos run --vcpus 2 --memory 512

  # Start in background
  fc-macos run --background`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMicroVM(cmd.Context(), vcpus, memoryMiB, kernel, rootfs, bootArgs, background)
		},
	}

	cmd.Flags().IntVar(&vcpus, "vcpus", 2, "number of vCPUs for the microVM")
	cmd.Flags().IntVar(&memoryMiB, "memory", 256, "memory in MiB for the microVM")
	cmd.Flags().StringVar(&kernel, "kernel", "/var/lib/firecracker/kernels/vmlinux", "path to kernel inside the VM")
	cmd.Flags().StringVar(&rootfs, "rootfs", "/var/lib/firecracker/rootfs/ubuntu-rw.ext4", "path to rootfs inside the VM")
	cmd.Flags().StringVar(&bootArgs, "boot-args", "console=ttyS0 reboot=k panic=1 pci=off", "kernel boot arguments")
	cmd.Flags().BoolVar(&background, "background", false, "run in background")

	return cmd
}

func runMicroVM(ctx context.Context, vcpus, memoryMiB int, kernel, rootfs, bootArgs string, background bool) error {
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
	client := &http.Client{Timeout: 5 * time.Second}

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

	// Check if Firecracker is already running and stop it
	resp, err := client.Get(agentURL + "/agent/status")
	if err == nil {
		defer resp.Body.Close()
		var status map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&status) == nil {
			if running, ok := status["firecracker_running"].(bool); ok && running {
				logrus.Info("Stopping existing Firecracker instance...")
				req, _ := http.NewRequestWithContext(ctx, "POST", agentURL+"/agent/stop", nil)
				if stopResp, err := client.Do(req); err == nil {
					stopResp.Body.Close()
					// Wait a moment for cleanup
					time.Sleep(500 * time.Millisecond)
				}
			}
		}
	}

	// Configure boot source
	logrus.Info("Configuring boot source...")
	bootSourceBody := fmt.Sprintf(`{
		"kernel_image_path": "%s",
		"boot_args": "%s"
	}`, kernel, bootArgs)

	if err := doAgentRequest(ctx, client, agentURL, "PUT", "/boot-source", bootSourceBody); err != nil {
		return fmt.Errorf("failed to set boot source: %w", err)
	}

	// Configure rootfs drive
	logrus.Info("Configuring rootfs drive...")
	driveBody := fmt.Sprintf(`{
		"drive_id": "rootfs",
		"path_on_host": "%s",
		"is_root_device": true,
		"is_read_only": false
	}`, rootfs)

	if err := doAgentRequest(ctx, client, agentURL, "PUT", "/drives/rootfs", driveBody); err != nil {
		return fmt.Errorf("failed to set rootfs drive: %w", err)
	}

	// Configure machine
	logrus.Info("Configuring machine...")
	machineBody := fmt.Sprintf(`{
		"vcpu_count": %d,
		"mem_size_mib": %d,
		"track_dirty_pages": true
	}`, vcpus, memoryMiB)

	if err := doAgentRequest(ctx, client, agentURL, "PUT", "/machine-config", machineBody); err != nil {
		return fmt.Errorf("failed to set machine config: %w", err)
	}

	// Start microVM
	logrus.Info("Starting microVM...")
	if err := doAgentRequest(ctx, client, agentURL, "PUT", "/actions", `{"action_type": "InstanceStart"}`); err != nil {
		return fmt.Errorf("failed to start microVM: %w", err)
	}

	fmt.Println()
	fmt.Println("=== MicroVM Started ===")
	fmt.Printf("vCPUs:  %d\n", vcpus)
	fmt.Printf("Memory: %d MiB\n", memoryMiB)
	fmt.Printf("Kernel: %s\n", kernel)
	fmt.Printf("Rootfs: %s\n", rootfs)
	fmt.Println()

	if background {
		fmt.Println("Commands:")
		fmt.Println("  fc-macos microvm status   # Check microVM status")
		fmt.Println("  fc-macos microvm shell    # Open shell to microVM")
		fmt.Println("  fc-macos microvm stop     # Stop the microVM")
		fmt.Println()
		logrus.Info("Running in background")
		return nil
	}

	// Connect to serial console
	fmt.Println("Connecting to serial console...")
	fmt.Println("Press Ctrl+] to exit")
	fmt.Println()

	return connectToConsole(ctx, agentURL)
}

func connectToConsole(ctx context.Context, agentURL string) error {
	// Connect to the console endpoint
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
