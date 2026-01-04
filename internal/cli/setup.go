package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const setupScript = `#!/bin/bash
set -e

FC_VERSION="${FC_VERSION:-1.10.1}"
ARCH="aarch64"

echo "=== Setting up fc-macos Linux VM ==="

# Update system
echo "Updating system packages..."
sudo apt-get update -qq
sudo apt-get install -y -qq curl jq socat

# Create directories
echo "Creating directories..."
sudo mkdir -p /usr/local/bin
sudo mkdir -p /var/lib/firecracker/{kernels,rootfs,snapshots}
sudo mkdir -p /etc/fc-agent

# Download Firecracker
echo "Downloading Firecracker ${FC_VERSION}..."
FC_URL="https://github.com/firecracker-microvm/firecracker/releases/download/v${FC_VERSION}/firecracker-v${FC_VERSION}-${ARCH}.tgz"
curl -sL "$FC_URL" -o /tmp/firecracker.tgz
tar -xzf /tmp/firecracker.tgz -C /tmp

# Install Firecracker
echo "Installing Firecracker..."
sudo mv /tmp/release-v${FC_VERSION}-${ARCH}/firecracker-v${FC_VERSION}-${ARCH} /usr/local/bin/firecracker
sudo chmod +x /usr/local/bin/firecracker

# Verify installation
echo "Verifying Firecracker installation..."
/usr/local/bin/firecracker --version

# Check KVM access
echo "Checking KVM access..."
if [ -c /dev/kvm ]; then
    echo "KVM is available"
    sudo chmod 666 /dev/kvm
else
    echo "ERROR: KVM device not found!"
    exit 1
fi

# Download sample kernel
echo "Downloading kernel..."
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/vmlinux-6.1.102"
sudo curl -sL "$KERNEL_URL" -o /var/lib/firecracker/kernels/vmlinux

# Download sample rootfs
echo "Downloading rootfs..."
ROOTFS_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/ubuntu-24.04.ext4"
sudo curl -sL "$ROOTFS_URL" -o /var/lib/firecracker/rootfs/ubuntu.ext4

# Create writable rootfs copy
echo "Creating writable rootfs copy..."
sudo cp /var/lib/firecracker/rootfs/ubuntu.ext4 /var/lib/firecracker/rootfs/ubuntu-rw.ext4
sudo chmod 666 /var/lib/firecracker/rootfs/ubuntu-rw.ext4

echo "=== Setup complete ==="
`

func newSetupCmd() *cobra.Command {
	var (
		force      bool
		skipAssets bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up the Linux VM with Firecracker",
		Long: `Initialize the intermediate Linux VM with Firecracker and required assets.

This command will:
1. Ensure the Tart VM exists (clone from Ubuntu if needed)
2. Start the VM with nested virtualization
3. Install Firecracker inside the VM
4. Download a sample kernel and rootfs
5. Install and start the fc-agent`,
		Example: `  # Initial setup
  fc-macos setup

  # Force re-setup (reinstall everything)
  fc-macos setup --force

  # Setup without downloading kernel/rootfs (use your own)
  fc-macos setup --skip-assets`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd.Context(), force, skipAssets)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force re-setup even if already configured")
	cmd.Flags().BoolVar(&skipAssets, "skip-assets", false, "skip downloading kernel and rootfs")

	return cmd
}

func runSetup(ctx context.Context, force, skipAssets bool) error {
	tartPath := findTart()
	if tartPath == "" {
		return fmt.Errorf("tart not found. Install from: https://github.com/cirruslabs/tart/releases")
	}

	vmName := "fc-macos-linux"

	// Check if VM exists
	logrus.Info("Checking for existing VM...")
	vmExists := checkVMExists(ctx, tartPath, vmName)

	if !vmExists {
		logrus.Info("Creating VM from Ubuntu image...")
		// First check if we have the base image
		if err := ensureBaseImage(ctx, tartPath); err != nil {
			return fmt.Errorf("failed to get base image: %w", err)
		}

		// Clone the VM
		cloneCmd := exec.CommandContext(ctx, tartPath, "clone", "ghcr.io/cirruslabs/ubuntu:latest", vmName)
		cloneCmd.Stdout = os.Stdout
		cloneCmd.Stderr = os.Stderr
		if err := cloneCmd.Run(); err != nil {
			return fmt.Errorf("failed to clone VM: %w", err)
		}
	}

	// Configure VM
	logrus.Info("Configuring VM (4 CPUs, 4GB RAM)...")
	setCmd := exec.CommandContext(ctx, tartPath, "set", vmName, "--cpu", "4", "--memory", "4096")
	if err := setCmd.Run(); err != nil {
		return fmt.Errorf("failed to configure VM: %w", err)
	}

	// Check if VM is already running
	if isVMRunning(ctx, tartPath, vmName) {
		if !force {
			logrus.Info("VM is already running")
		} else {
			logrus.Info("Stopping existing VM...")
			exec.CommandContext(ctx, tartPath, "stop", vmName).Run()
			time.Sleep(2 * time.Second)
		}
	}

	// Start VM with nested virtualization
	logrus.Info("Starting VM with nested virtualization...")
	runCmd := exec.CommandContext(ctx, tartPath, "run", vmName, "--no-graphics", "--nested")
	runCmd.Stdout = nil
	runCmd.Stderr = nil
	if err := runCmd.Start(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	// Wait for VM to be ready
	logrus.Info("Waiting for VM to be ready...")
	var vmIP string
	for i := 0; i < 60; i++ {
		ipCmd := exec.CommandContext(ctx, tartPath, "ip", vmName)
		output, err := ipCmd.Output()
		if err == nil && len(output) > 0 {
			vmIP = strings.TrimSpace(string(output))
			break
		}
		time.Sleep(2 * time.Second)
	}

	if vmIP == "" {
		return fmt.Errorf("VM did not get an IP address")
	}

	logrus.Infof("VM ready at %s", vmIP)

	// Wait for SSH/exec to be available
	logrus.Info("Waiting for VM to accept commands...")
	for i := 0; i < 30; i++ {
		testCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "echo", "ready")
		if err := testCmd.Run(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Check if setup is needed
	checkCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "test", "-f", "/usr/local/bin/firecracker")
	alreadySetup := checkCmd.Run() == nil

	if alreadySetup && !force {
		logrus.Info("Firecracker is already installed")
	} else {
		// Run setup script
		logrus.Info("Installing Firecracker and dependencies...")

		script := setupScript
		if skipAssets {
			// Remove the download parts from the script
			script = strings.Replace(script, `# Download sample kernel
echo "Downloading kernel..."
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/vmlinux-6.1.102"
sudo curl -sL "$KERNEL_URL" -o /var/lib/firecracker/kernels/vmlinux

# Download sample rootfs
echo "Downloading rootfs..."
ROOTFS_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/ubuntu-24.04.ext4"
sudo curl -sL "$ROOTFS_URL" -o /var/lib/firecracker/rootfs/ubuntu.ext4

# Create writable rootfs copy
echo "Creating writable rootfs copy..."
sudo cp /var/lib/firecracker/rootfs/ubuntu.ext4 /var/lib/firecracker/rootfs/ubuntu-rw.ext4
sudo chmod 666 /var/lib/firecracker/rootfs/ubuntu-rw.ext4`, "", 1)
		}

		// Transfer setup script via HTTP (tart exec stdin doesn't work reliably)
		if err := transferScriptViaHTTP(ctx, tartPath, vmName, vmIP, script); err != nil {
			return fmt.Errorf("failed to transfer setup script: %w", err)
		}

		setupCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "/tmp/setup.sh")
		setupCmd.Stdout = os.Stdout
		setupCmd.Stderr = os.Stderr
		if err := setupCmd.Run(); err != nil {
			return fmt.Errorf("setup script failed: %w", err)
		}
	}

	// Copy and start fc-agent
	agentPath := findAgent()
	if agentPath != "" {
		logrus.Info("Installing fc-agent...")

		// Use scp to copy the agent binary (Ubuntu image uses admin:admin)
		scpCmd := exec.CommandContext(ctx, "sshpass", "-p", "admin",
			"scp", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			agentPath, fmt.Sprintf("admin@%s:/tmp/fc-agent", vmIP))
		if output, err := scpCmd.CombinedOutput(); err != nil {
			logrus.Warnf("Could not scp fc-agent (sshpass may not be installed): %v, output: %s", err, string(output))

			// Fallback: Try using Python to serve the file
			logrus.Info("Trying HTTP file transfer fallback...")
			if err := transferAgentViaHTTP(ctx, tartPath, vmName, vmIP, agentPath); err != nil {
				logrus.Warnf("HTTP transfer failed: %v", err)
			}
		} else {
			// Move to final location and make executable
			exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "mv", "/tmp/fc-agent", "/usr/local/bin/fc-agent").Run()
			exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "chmod", "+x", "/usr/local/bin/fc-agent").Run()
		}

		// Verify installation
		verifyInstallCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
			"ls -la /usr/local/bin/fc-agent && /usr/local/bin/fc-agent --version")
		if output, err := verifyInstallCmd.CombinedOutput(); err != nil {
			logrus.Warnf("fc-agent verification failed: %v, output: %s", err, string(output))
		} else {
			logrus.Debugf("fc-agent installed: %s", string(output))
		}

		// Start agent
		logrus.Info("Starting fc-agent...")
		exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
			"sudo pkill fc-agent 2>/dev/null || true").Run()
		exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
			"nohup sudo /usr/local/bin/fc-agent > /var/log/fc-agent.log 2>&1 &").Run()

		// Wait for agent to be ready
		time.Sleep(2 * time.Second)

		// Verify agent is running
		checkCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
			"pgrep -f fc-agent")
		if output, err := checkCmd.Output(); err != nil {
			logrus.Warn("fc-agent may not have started correctly")
		} else {
			logrus.Debugf("fc-agent running with PID: %s", strings.TrimSpace(string(output)))
		}
	} else {
		logrus.Warn("fc-agent binary not found, skipping agent installation")
	}

	fmt.Println()
	fmt.Println("=== Setup Complete ===")
	fmt.Println()
	fmt.Printf("VM Name: %s\n", vmName)
	fmt.Printf("VM IP:   %s\n", vmIP)
	fmt.Println()
	fmt.Println("Kernel:  /var/lib/firecracker/kernels/vmlinux")
	fmt.Println("Rootfs:  /var/lib/firecracker/rootfs/ubuntu-rw.ext4")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  fc-macos run              # Start a microVM with defaults")
	fmt.Println("  fc-macos microvm shell    # Open shell to microVM")
	fmt.Println("  fc-macos microvm status   # Check microVM status")
	fmt.Println()

	return nil
}

func findTart() string {
	paths := []string{
		"tart",
		filepath.Join(os.Getenv("HOME"), "Applications/tart.app/Contents/MacOS/tart"),
		"/Applications/tart.app/Contents/MacOS/tart",
		"/usr/local/bin/tart",
		"/opt/homebrew/bin/tart",
	}

	for _, p := range paths {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func findAgent() string {
	// Look for fc-agent binary
	paths := []string{
		"build/fc-agent-linux-arm64",
		"./build/fc-agent-linux-arm64",
		filepath.Join(os.Getenv("HOME"), ".fc-macos/fc-agent"),
		"/usr/local/share/fc-macos/fc-agent",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func checkVMExists(ctx context.Context, tartPath, vmName string) bool {
	cmd := exec.CommandContext(ctx, tartPath, "list")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), vmName)
}

func isVMRunning(ctx context.Context, tartPath, vmName string) bool {
	cmd := exec.CommandContext(ctx, tartPath, "list")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, vmName) && strings.Contains(line, "running") {
			return true
		}
	}
	return false
}

func ensureBaseImage(ctx context.Context, tartPath string) error {
	// Check if image exists
	cmd := exec.CommandContext(ctx, tartPath, "list")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "ghcr.io/cirruslabs/ubuntu") {
		return nil
	}

	// Pull the image
	logrus.Info("Pulling Ubuntu image (this may take a few minutes)...")
	pullCmd := exec.CommandContext(ctx, tartPath, "pull", "ghcr.io/cirruslabs/ubuntu:latest")
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	return pullCmd.Run()
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func transferScriptViaHTTP(ctx context.Context, tartPath, vmName, vmIP, script string) error {
	// Get the host IP that the VM can reach
	hostIP := getHostIPForVM(vmIP)
	if hostIP == "" {
		return fmt.Errorf("could not determine host IP")
	}

	// Start HTTP server on a random port
	listener, err := net.Listen("tcp", hostIP+":0")
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	logrus.Debugf("Starting temporary HTTP server at %s for script transfer", serverAddr)

	// Serve the script
	go func() {
		http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(script))
		}))
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Download from VM
	downloadCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
		fmt.Sprintf("curl -sL 'http://%s/' -o /tmp/setup.sh && chmod +x /tmp/setup.sh", serverAddr))
	if output, err := downloadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("download failed: %v, output: %s", err, string(output))
	}

	return nil
}

func transferAgentViaHTTP(ctx context.Context, tartPath, vmName, vmIP, agentPath string) error {
	// Start a temporary HTTP server to serve the agent binary
	// This is a workaround for tart exec not handling stdin well

	agentData, err := os.ReadFile(agentPath)
	if err != nil {
		return fmt.Errorf("failed to read agent: %w", err)
	}

	// Get the host IP that the VM can reach (use the same network as the VM IP)
	hostIP := getHostIPForVM(vmIP)
	if hostIP == "" {
		return fmt.Errorf("could not determine host IP")
	}

	// Start HTTP server on a random port
	listener, err := net.Listen("tcp", hostIP+":0")
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	logrus.Debugf("Starting temporary HTTP server at %s", serverAddr)

	// Serve the agent binary
	go func() {
		http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(agentData)))
			w.Write(agentData)
		}))
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Download from VM
	downloadCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
		fmt.Sprintf("curl -sL 'http://%s/fc-agent' -o /tmp/fc-agent", serverAddr))
	if output, err := downloadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("download failed: %v, output: %s", err, string(output))
	}

	// Move to final location
	exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "mv", "/tmp/fc-agent", "/usr/local/bin/fc-agent").Run()
	exec.CommandContext(ctx, tartPath, "exec", vmName, "sudo", "chmod", "+x", "/usr/local/bin/fc-agent").Run()

	return nil
}

func getHostIPForVM(vmIP string) string {
	// Get network interfaces and find one on the same subnet as the VM
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	vmIPAddr := net.ParseIP(vmIP)
	if vmIPAddr == nil {
		return ""
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipNet.IP.To4() == nil {
				continue
			}
			// Check if VM IP is in the same network
			if ipNet.Contains(vmIPAddr) {
				return ipNet.IP.String()
			}
		}
	}

	// Fallback to any non-loopback interface
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}

	return ""
}
