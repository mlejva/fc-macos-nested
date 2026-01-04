package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

const tartVMName = "fc-macos-linux"

func newVMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage the intermediate Linux VM",
		Long: `Control the Linux VM that runs between macOS and Firecracker.
This VM provides KVM support via nested virtualization.`,
	}

	cmd.AddCommand(newVMStatusCmd())
	cmd.AddCommand(newVMLogsCmd())
	cmd.AddCommand(newVMShellCmd())
	cmd.AddCommand(newVMStartCmd())
	cmd.AddCommand(newVMStopCmd())

	return cmd
}

func newVMStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the status of the Linux VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tartPath := findTart()
			if tartPath == "" {
				return fmt.Errorf("tart not found")
			}

			// Get VM list
			listCmd := exec.CommandContext(ctx, tartPath, "list")
			output, err := listCmd.Output()
			if err != nil {
				return fmt.Errorf("failed to list VMs: %w", err)
			}

			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, tartVMName) {
					fmt.Println("=== Linux VM Status ===")
					fmt.Printf("Name: %s\n", tartVMName)
					if strings.Contains(line, "running") {
						fmt.Println("State: running")
						// Get IP
						ipCmd := exec.CommandContext(ctx, tartPath, "ip", tartVMName)
						if ipOut, err := ipCmd.Output(); err == nil {
							fmt.Printf("IP: %s\n", strings.TrimSpace(string(ipOut)))
						}
					} else {
						fmt.Println("State: stopped")
					}
					return nil
				}
			}

			fmt.Println("VM not found. Run 'fc-macos setup' to create it.")
			return nil
		},
	}
}

func newVMLogsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View logs from the Linux VM (fc-agent logs)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tartPath := findTart()
			if tartPath == "" {
				return fmt.Errorf("tart not found")
			}

			if !isVMRunning(ctx, tartPath, tartVMName) {
				return fmt.Errorf("VM is not running")
			}

			var logCmd *exec.Cmd
			if follow {
				logCmd = exec.CommandContext(ctx, tartPath, "exec", tartVMName, "tail", "-f", "/var/log/fc-agent.log")
			} else {
				logCmd = exec.CommandContext(ctx, tartPath, "exec", tartVMName, "cat", "/var/log/fc-agent.log")
			}
			logCmd.Stdout = os.Stdout
			logCmd.Stderr = os.Stderr
			return logCmd.Run()
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")

	return cmd
}

func newVMShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive shell to the Linux VM",
		Long: `Open an interactive bash shell inside the Linux VM.
This is useful for debugging and manual operations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tartPath := findTart()
			if tartPath == "" {
				return fmt.Errorf("tart not found")
			}

			if !isVMRunning(ctx, tartPath, tartVMName) {
				return fmt.Errorf("VM is not running. Run 'fc-macos setup' first")
			}

			fmt.Println("Connecting to Linux VM...")
			fmt.Println("Type 'exit' to disconnect")
			fmt.Println()

			shellCmd := exec.CommandContext(ctx, tartPath, "exec", tartVMName, "bash")
			shellCmd.Stdin = os.Stdin
			shellCmd.Stdout = os.Stdout
			shellCmd.Stderr = os.Stderr
			return shellCmd.Run()
		},
	}
}

func newVMStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Linux VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tartPath := findTart()
			if tartPath == "" {
				return fmt.Errorf("tart not found")
			}

			if isVMRunning(ctx, tartPath, tartVMName) {
				fmt.Println("VM is already running")
				return nil
			}

			if !checkVMExists(ctx, tartPath, tartVMName) {
				return fmt.Errorf("VM does not exist. Run 'fc-macos setup' to create it")
			}

			fmt.Println("Starting Linux VM...")
			startCmd := exec.CommandContext(ctx, tartPath, "run", tartVMName, "--no-graphics", "--nested")
			if err := startCmd.Start(); err != nil {
				return fmt.Errorf("failed to start VM: %w", err)
			}

			fmt.Println("Linux VM started")
			return nil
		},
	}
}

func newVMStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the Linux VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tartPath := findTart()
			if tartPath == "" {
				return fmt.Errorf("tart not found")
			}

			if !isVMRunning(ctx, tartPath, tartVMName) {
				fmt.Println("VM is not running")
				return nil
			}

			fmt.Println("Stopping Linux VM...")
			var stopCmd *exec.Cmd
			if force {
				stopCmd = exec.CommandContext(ctx, tartPath, "stop", "--force", tartVMName)
			} else {
				stopCmd = exec.CommandContext(ctx, tartPath, "stop", tartVMName)
			}

			if err := stopCmd.Run(); err != nil {
				return fmt.Errorf("failed to stop VM: %w", err)
			}

			fmt.Println("Linux VM stopped")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force stop the VM")

	return cmd
}

