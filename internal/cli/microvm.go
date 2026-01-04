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
		Short: "Manage Firecracker microVMs",
		Long:  `Commands to manage Firecracker microVMs running inside the Linux VM.`,
	}

	cmd.AddCommand(newMicroVMListCmd())
	cmd.AddCommand(newMicroVMStatusCmd())
	cmd.AddCommand(newMicroVMShellCmd())
	cmd.AddCommand(newMicroVMStopCmd())
	cmd.AddCommand(newMicroVMLogsCmd())

	return cmd
}

func newMicroVMListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all microVMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listMicroVMs(cmd.Context())
		},
	}
}

func newMicroVMStatusCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show microVM status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMicroVMStatus(cmd.Context(), name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "microVM name or ID (shows all if not specified)")

	return cmd
}

func newMicroVMShellCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open interactive shell to a microVM",
		Long: `Open an interactive shell session to a Firecracker microVM.

This connects to the microVM's serial console via the intermediate Linux VM.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return openMicroVMShell(cmd.Context(), name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "microVM name or ID (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}

func newMicroVMStopCmd() *cobra.Command {
	var (
		name  string
		force bool
		all   bool
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a microVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopMicroVM(cmd.Context(), name, force, all)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "microVM name or ID")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "force stop (kill process)")
	cmd.Flags().BoolVar(&all, "all", false, "stop all microVMs")

	return cmd
}

func newMicroVMLogsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show fc-agent logs",
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

// listMicroVMs lists all microVMs
func listMicroVMs(ctx context.Context) error {
	_, agentURL, client, err := getVMConnection(ctx)
	if err != nil {
		return err
	}

	resp, err := client.Get(agentURL + "/agent/microvms")
	if err != nil {
		return fmt.Errorf("failed to list microVMs: %w", err)
	}
	defer resp.Body.Close()

	var vms []MicroVMInfo
	if err := json.NewDecoder(resp.Body).Decode(&vms); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if len(vms) == 0 {
		fmt.Println("No microVMs running")
		fmt.Println()
		fmt.Println("Start a microVM with:")
		fmt.Println("  fc-macos run --name my-vm")
		return nil
	}

	fmt.Printf("%-20s %-18s %-10s %-6s %-10s %s\n", "NAME", "ID", "STATUS", "VCPUS", "MEMORY", "CREATED")
	fmt.Println(strings.Repeat("-", 85))

	for _, vm := range vms {
		status := "stopped"
		if vm.Running {
			status = "running"
		}
		id := vm.ID
		if len(id) > 15 {
			id = id[:15] + "..."
		}
		vcpus := 0
		memory := 0
		if vm.Config != nil {
			vcpus = vm.Config.VCPUs
			memory = vm.Config.MemoryMiB
		}
		fmt.Printf("%-20s %-18s %-10s %-6d %-10d %s\n",
			vm.Name, id, status, vcpus, memory,
			vm.CreatedAt.Format("15:04:05"))
	}

	fmt.Println()
	running := 0
	for _, vm := range vms {
		if vm.Running {
			running++
		}
	}
	fmt.Printf("%d microVM(s), %d running\n", len(vms), running)

	return nil
}

// resolveVMName resolves a name or ID to a full VM ID
func resolveVMName(ctx context.Context, client *http.Client, agentURL, name string) (string, error) {
	resp, err := client.Get(agentURL + "/agent/microvms")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var vms []MicroVMInfo
	if err := json.NewDecoder(resp.Body).Decode(&vms); err != nil {
		return "", err
	}

	for _, vm := range vms {
		if vm.Name == name || vm.ID == name || strings.HasPrefix(vm.ID, name) {
			return vm.ID, nil
		}
	}

	return "", fmt.Errorf("microVM not found: %s", name)
}

func showMicroVMStatus(ctx context.Context, name string) error {
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

	// If no specific VM requested, list all
	if name == "" {
		return listMicroVMs(ctx)
	}

	// Get specific VM status
	vmID, err := resolveVMName(ctx, client, agentURL, name)
	if err != nil {
		return err
	}

	resp, err = client.Get(fmt.Sprintf("%s/agent/microvms/%s", agentURL, vmID))
	if err != nil {
		return fmt.Errorf("failed to get microVM status: %w", err)
	}
	defer resp.Body.Close()

	var vm MicroVMInfo
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("=== MicroVM: %s ===\n", vm.Name)
	fmt.Printf("ID:      %s\n", vm.ID)
	status := "stopped"
	if vm.Running {
		status = "running"
	}
	fmt.Printf("Status:  %s\n", status)
	if vm.PID > 0 {
		fmt.Printf("PID:     %d\n", vm.PID)
	}
	if vm.Config != nil {
		fmt.Printf("vCPUs:   %d\n", vm.Config.VCPUs)
		fmt.Printf("Memory:  %d MiB\n", vm.Config.MemoryMiB)
		fmt.Printf("Kernel:  %s\n", vm.Config.Kernel)
		fmt.Printf("Rootfs:  %s\n", vm.Config.Rootfs)
	}
	fmt.Printf("Created: %s\n", vm.CreatedAt.Format(time.RFC3339))

	return nil
}

func openMicroVMShell(ctx context.Context, name string) error {
	_, agentURL, client, err := getVMConnection(ctx)
	if err != nil {
		return err
	}

	// Resolve VM name to ID
	vmID, err := resolveVMName(ctx, client, agentURL, name)
	if err != nil {
		return err
	}

	fmt.Println("=== MicroVM Shell ===")
	fmt.Printf("Connecting to %s...\n", name)
	fmt.Println()
	fmt.Println("Press Ctrl+] to exit the shell.")
	fmt.Println()

	// Connect to the VM-specific console
	return connectToVMConsole(ctx, agentURL, vmID)
}

func stopMicroVM(ctx context.Context, name string, force, all bool) error {
	_, agentURL, client, err := getVMConnection(ctx)
	if err != nil {
		return err
	}

	if all {
		// Stop all VMs
		resp, err := client.Get(agentURL + "/agent/microvms")
		if err != nil {
			return fmt.Errorf("failed to list microVMs: %w", err)
		}

		var vms []MicroVMInfo
		json.NewDecoder(resp.Body).Decode(&vms)
		resp.Body.Close()

		if len(vms) == 0 {
			fmt.Println("No microVMs to stop")
			return nil
		}

		for _, vm := range vms {
			if err := stopSingleVM(ctx, client, agentURL, vm.ID, force); err != nil {
				logrus.Warnf("Failed to stop %s: %v", vm.Name, err)
			} else {
				fmt.Printf("Stopped: %s\n", vm.Name)
			}
		}
		return nil
	}

	if name == "" {
		return fmt.Errorf("--name is required (or use --all to stop all microVMs)")
	}

	// Resolve VM name to ID
	vmID, err := resolveVMName(ctx, client, agentURL, name)
	if err != nil {
		return err
	}

	if err := stopSingleVM(ctx, client, agentURL, vmID, force); err != nil {
		return err
	}

	fmt.Printf("Stopped: %s\n", name)
	return nil
}

func stopSingleVM(ctx context.Context, client *http.Client, agentURL, vmID string, force bool) error {
	url := fmt.Sprintf("%s/agent/microvms/%s", agentURL, vmID)
	if force {
		url += "?force=true"
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stop failed: %s", string(body))
	}

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
