//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fcMacosBinary string

func init() {
	if bin := os.Getenv("FC_MACOS_BIN"); bin != "" {
		fcMacosBinary = bin
	} else {
		// Default to project root relative path
		fcMacosBinary = "../../build/fc-macos"
	}
}

// TestCLIVersion tests the version command.
func TestCLIVersion(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "fc-macos version")
	assert.Contains(t, out, "Go version")
	assert.Contains(t, out, "OS/Arch")
}

// TestCLIHelp tests the help command.
func TestCLIHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "fc-macos")
	assert.Contains(t, out, "Firecracker")
	assert.Contains(t, out, "Commands:")
}

// TestCLIBootHelp tests the boot command help.
func TestCLIBootHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "boot", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "boot")
	assert.Contains(t, out, "set")
	assert.Contains(t, out, "get")
}

// TestCLIDrivesHelp tests the drives command help.
func TestCLIDrivesHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "drives", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "drives")
	assert.Contains(t, out, "add")
	assert.Contains(t, out, "list")
}

// TestCLINetworkHelp tests the network command help.
func TestCLINetworkHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "network", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "network")
	assert.Contains(t, out, "add")
	assert.Contains(t, out, "list")
}

// TestCLIMachineHelp tests the machine command help.
func TestCLIMachineHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "machine", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "machine")
	assert.Contains(t, out, "config")
	assert.Contains(t, out, "info")
}

// TestCLIActionsHelp tests the actions command help.
func TestCLIActionsHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "actions", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "actions")
	assert.Contains(t, out, "start")
	assert.Contains(t, out, "stop")
}

// TestCLISnapshotsHelp tests the snapshots command help.
func TestCLISnapshotsHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "snapshots", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "snapshot")
	assert.Contains(t, out, "create")
	assert.Contains(t, out, "load")
}

// TestCLIMetricsHelp tests the metrics command help.
func TestCLIMetricsHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "metrics", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "metrics")
	assert.Contains(t, out, "get")
}

// TestCLIBalloonHelp tests the balloon command help.
func TestCLIBalloonHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "balloon", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "balloon")
	assert.Contains(t, out, "set")
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "stats")
}

// TestCLIVMHelp tests the vm command help.
func TestCLIVMHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "vm", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "vm")
	assert.Contains(t, out, "status")
	assert.Contains(t, out, "start")
	assert.Contains(t, out, "stop")
}

// TestCLIBootSetRequiresKernel tests that boot set requires --kernel flag.
func TestCLIBootSetRequiresKernel(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "boot", "set")
	require.Error(t, err)
	// The error message should mention kernel in stdout/stderr
	assert.Contains(t, out, "kernel")
}

// TestCLIDrivesAddRequiresIdAndPath tests that drives add requires flags.
func TestCLIDrivesAddRequiresIdAndPath(t *testing.T) {
	_, err := runCommand(fcMacosBinary, "drives", "add")
	require.Error(t, err)
}

// Integration tests that require a running VM

// TestVMStatus tests the vm status command.
func TestVMStatus(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "vm", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Linux VM Status")
	assert.Contains(t, out, "fc-macos-linux")
}

// TestMicroVMStatus tests the microvm status command.
func TestMicroVMStatus(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "microvm", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Linux VM Status")
	assert.Contains(t, out, "fc-agent Status")
}

// TestSetupHelp tests the setup command help.
func TestSetupHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "setup", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "setup")
	assert.Contains(t, out, "Firecracker")
	assert.Contains(t, out, "--force")
}

// TestRunHelp tests the run command help.
func TestRunHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "run", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "run")
	assert.Contains(t, out, "--vcpus")
	assert.Contains(t, out, "--memory")
	assert.Contains(t, out, "--kernel")
	assert.Contains(t, out, "--rootfs")
}

// TestMicroVMHelp tests the microvm command help.
func TestMicroVMHelp(t *testing.T) {
	out, err := runCommand(fcMacosBinary, "microvm", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "microvm")
	assert.Contains(t, out, "status")
	assert.Contains(t, out, "shell")
	assert.Contains(t, out, "stop")
	assert.Contains(t, out, "logs")
}

// TestFullWorkflow tests the complete workflow: setup, run, status, stop
func TestFullWorkflow(t *testing.T) {
	if os.Getenv("FC_E2E_FULL") != "1" {
		t.Skip("Skipping full workflow test (set FC_E2E_FULL=1 to run)")
	}

	// Stop any existing microVM (ignore errors)
	runCommand(fcMacosBinary, "microvm", "stop", "--force")

	// Small delay to ensure cleanup
	time.Sleep(2 * time.Second)

	// Run microVM in background
	out, err := runCommand(fcMacosBinary, "run", "--background",
		"--rootfs", "/var/lib/firecracker/rootfs/alpine-shell.ext4",
		"--boot-args", "console=ttyS0 reboot=k panic=1 pci=off init=/init")
	require.NoError(t, err, "Failed to start microVM: %s", out)
	assert.Contains(t, out, "MicroVM Started")

	// Wait for microVM to boot
	time.Sleep(3 * time.Second)

	// Check status
	out, err = runCommand(fcMacosBinary, "microvm", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "fc-agent Status")
	assert.Contains(t, out, "healthy")

	// Stop microVM (force to ensure it works)
	out, err = runCommand(fcMacosBinary, "microvm", "stop", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "stopped")
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}

	if err != nil {
		return output, err
	}
	return strings.TrimSpace(output), nil
}
