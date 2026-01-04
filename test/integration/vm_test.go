//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anthropics/fc-macos/internal/linuxvm"
	"github.com/stretchr/testify/require"
)

// TestLinuxVMCreate tests that we can create a VM configuration.
// This test requires a kernel and rootfs to be present.
func TestLinuxVMCreate(t *testing.T) {
	kernelPath := os.Getenv("FC_TEST_KERNEL")
	rootfsPath := os.Getenv("FC_TEST_ROOTFS")

	if kernelPath == "" || rootfsPath == "" {
		t.Skip("FC_TEST_KERNEL and FC_TEST_ROOTFS must be set for integration tests")
	}

	cfg := &linuxvm.Config{
		KernelPath:    kernelPath,
		RootFSPath:    rootfsPath,
		CPUCount:      2,
		MemorySizeMiB: 2048,
		EnableNested:  true,
		VsockPort:     2222,
	}

	vm, err := linuxvm.New(cfg)
	require.NoError(t, err)
	require.NotNil(t, vm)
	require.Equal(t, linuxvm.StateNotStarted, vm.State())
}

// TestLinuxVMLifecycle tests the full VM lifecycle.
// This test requires real hardware and kernel/rootfs.
func TestLinuxVMLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping VM lifecycle test in short mode")
	}

	kernelPath := os.Getenv("FC_TEST_KERNEL")
	rootfsPath := os.Getenv("FC_TEST_ROOTFS")

	if kernelPath == "" || rootfsPath == "" {
		t.Skip("FC_TEST_KERNEL and FC_TEST_ROOTFS must be set for integration tests")
	}

	cfg := &linuxvm.Config{
		KernelPath:    kernelPath,
		RootFSPath:    rootfsPath,
		CPUCount:      2,
		MemorySizeMiB: 2048,
		EnableNested:  true,
		VsockPort:     2222,
		BootArgs:      "console=hvc0 root=/dev/vda rw",
	}

	vm, err := linuxvm.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start VM
	err = vm.Start(ctx)
	require.NoError(t, err)
	require.Equal(t, linuxvm.StateRunning, vm.State())

	// Wait a bit for VM to boot
	time.Sleep(5 * time.Second)

	// Check vsock device is available
	vsock := vm.VsockDevice()
	require.NotNil(t, vsock, "vsock device should be available")

	// Stop VM
	err = vm.Stop(ctx, false)
	require.NoError(t, err)
	require.Equal(t, linuxvm.StateStopped, vm.State())
}
