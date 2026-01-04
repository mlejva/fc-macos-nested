package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newMicroVMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "microvm",
		Short: "Manage the Firecracker microVM",
		Long:  `Commands to manage the Firecracker microVM running inside the Linux VM.`,
	}

	cmd.AddCommand(newMicroVMStatusCmd())
	cmd.AddCommand(newMicroVMShellCmd())
	cmd.AddCommand(newMicroVMStopCmd())
	cmd.AddCommand(newMicroVMLogsCmd())

	return cmd
}

func newMicroVMStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show microVM status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMicroVMStatus(cmd.Context())
		},
	}
}

func newMicroVMShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Open interactive shell to the microVM",
		Long: `Open an interactive shell session to the Firecracker microVM.

This connects to the microVM's serial console via the intermediate Linux VM.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return openMicroVMShell(cmd.Context())
		},
	}
}

func newMicroVMStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the microVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopMicroVM(cmd.Context(), force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "force stop (kill process)")

	return cmd
}

func newMicroVMLogsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show microVM logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMicroVMLogs(cmd.Context(), follow)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")

	return cmd
}

func getVMConnection(ctx context.Context) (string, string, *http.Client, error) {
	tartPath := findTart()
	if tartPath == "" {
		return "", "", nil, fmt.Errorf("tart not found")
	}

	vmName := "fc-macos-linux"

	if !isVMRunning(ctx, tartPath, vmName) {
		return "", "", nil, fmt.Errorf("VM is not running. Run 'fc-macos setup' first")
	}

	ipCmd := exec.CommandContext(ctx, tartPath, "ip", vmName)
	output, err := ipCmd.Output()
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get VM IP: %w", err)
	}
	vmIP := strings.TrimSpace(string(output))

	client := &http.Client{Timeout: 10 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)

	return tartPath, agentURL, client, nil
}

func showMicroVMStatus(ctx context.Context) error {
	tartPath, agentURL, client, err := getVMConnection(ctx)
	if err != nil {
		return err
	}

	vmName := "fc-macos-linux"

	// Get VM IP for display
	ipCmd := exec.CommandContext(ctx, tartPath, "ip", vmName)
	output, _ := ipCmd.Output()
	vmIP := strings.TrimSpace(string(output))

	fmt.Println("=== Linux VM Status ===")
	fmt.Printf("Name:   %s\n", vmName)
	fmt.Printf("IP:     %s\n", vmIP)
	fmt.Printf("Status: running\n")
	fmt.Println()

	// Check agent health
	resp, err := client.Get(agentURL + "/health")
	if err != nil {
		fmt.Println("=== fc-agent Status ===")
		fmt.Printf("Status: not responding (%v)\n", err)
		return nil
	}
	defer resp.Body.Close()

	fmt.Println("=== fc-agent Status ===")
	fmt.Printf("Status: healthy\n")
	fmt.Printf("URL:    %s\n", agentURL)
	fmt.Println()

	// Get machine config
	resp, err = client.Get(agentURL + "/machine-config")
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var config map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&config) == nil {
			fmt.Println("=== MicroVM Configuration ===")
			if vcpus, ok := config["vcpu_count"].(float64); ok {
				fmt.Printf("vCPUs:  %.0f\n", vcpus)
			}
			if memory, ok := config["mem_size_mib"].(float64); ok {
				fmt.Printf("Memory: %.0f MiB\n", memory)
			}
			fmt.Println()
		}
	}

	// Try to get metrics to see if microVM is running
	resp, err = client.Get(agentURL + "/metrics")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			fmt.Println("=== MicroVM Status ===")
			fmt.Printf("Status: running\n")
		} else {
			fmt.Println("=== MicroVM Status ===")
			fmt.Printf("Status: not started or stopped\n")
		}
	}

	return nil
}

func openMicroVMShell(ctx context.Context) error {
	tartPath := findTart()
	if tartPath == "" {
		return fmt.Errorf("tart not found")
	}

	vmName := "fc-macos-linux"

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

	fmt.Println("=== MicroVM Shell ===")
	fmt.Println()
	fmt.Println("Connecting to microVM serial console...")
	fmt.Println("Note: The microVM must be running for the shell to work.")
	fmt.Println("If no output appears, the microVM may not have a shell on the serial console.")
	fmt.Println()
	fmt.Println("Press Ctrl+] to exit the shell.")
	fmt.Println()

	// Connect to the microVM's serial console via socat
	// The fc-agent should expose a serial console endpoint
	// For now, we'll connect via the VM and use socat to the Firecracker socket

	// First, check if the serial console is available
	client := &http.Client{Timeout: 5 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)

	resp, err := client.Get(agentURL + "/health")
	if err != nil {
		return fmt.Errorf("fc-agent not responding: %w", err)
	}
	resp.Body.Close()

	// Use tart exec to run socat to connect to the Firecracker serial console
	// The serial console is typically at /tmp/firecracker.socket.serial
	shellCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
		"socat -,raw,echo=0 UNIX-CONNECT:/tmp/firecracker.socket.serial 2>/dev/null || echo 'Serial console not available. Is the microVM running?'")
	shellCmd.Stdin = os.Stdin
	shellCmd.Stdout = os.Stdout
	shellCmd.Stderr = os.Stderr

	return shellCmd.Run()
}

func stopMicroVM(ctx context.Context, force bool) error {
	_, agentURL, client, err := getVMConnection(ctx)
	if err != nil {
		return err
	}

	if force {
		logrus.Info("Force stopping microVM...")
		// Kill the Firecracker process directly
		tartPath := findTart()
		vmName := "fc-macos-linux"
		killCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "pkill", "-9", "firecracker")
		killCmd.Run()
		fmt.Println("MicroVM force stopped")
		return nil
	}

	logrus.Info("Stopping microVM gracefully...")
	req, err := http.NewRequestWithContext(ctx, "PUT", agentURL+"/actions",
		strings.NewReader(`{"action_type": "SendCtrlAltDel"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to stop microVM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("stop request failed with status %d", resp.StatusCode)
	}

	fmt.Println("MicroVM stop signal sent")
	return nil
}

func showMicroVMLogs(ctx context.Context, follow bool) error {
	tartPath := findTart()
	if tartPath == "" {
		return fmt.Errorf("tart not found")
	}

	vmName := "fc-macos-linux"

	if !isVMRunning(ctx, tartPath, vmName) {
		return fmt.Errorf("VM is not running. Run 'fc-macos setup' first")
	}

	var cmd *exec.Cmd
	if follow {
		cmd = exec.CommandContext(ctx, tartPath, "exec", vmName, "tail", "-f", "/var/log/fc-agent.log")
	} else {
		cmd = exec.CommandContext(ctx, tartPath, "exec", vmName, "cat", "/var/log/fc-agent.log")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	if follow {
		// For follow mode, copy remaining output
		io.Copy(os.Stdout, stdout)
	}

	return cmd.Wait()
}
