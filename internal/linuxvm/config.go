// Package linuxvm manages the intermediate Linux VM that provides KVM for Firecracker.
package linuxvm

import (
	"fmt"
)

// Config holds the configuration for the Linux VM.
type Config struct {
	// KernelPath is the path to the Linux kernel image.
	KernelPath string

	// InitrdPath is the optional path to the initrd image.
	InitrdPath string

	// RootFSPath is the path to the root filesystem image.
	RootFSPath string

	// CPUCount is the number of virtual CPUs.
	CPUCount uint

	// MemorySizeMiB is the amount of memory in MiB.
	MemorySizeMiB uint64

	// EnableNested enables nested virtualization for KVM support.
	EnableNested bool

	// SharedDirs is a list of directories to share with the VM via virtio-fs.
	SharedDirs []SharedDir

	// VsockPort is the vsock port for host-guest communication.
	VsockPort uint32

	// BootArgs are additional kernel boot arguments.
	BootArgs string
}

// SharedDir represents a directory shared between host and guest.
type SharedDir struct {
	// Tag is the mount tag used to identify this share in the guest.
	Tag string

	// HostPath is the path on the host to share.
	HostPath string

	// ReadOnly mounts the share as read-only.
	ReadOnly bool
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.KernelPath == "" {
		return fmt.Errorf("kernel path is required")
	}
	if c.RootFSPath == "" {
		return fmt.Errorf("rootfs path is required")
	}
	if c.CPUCount == 0 {
		return fmt.Errorf("CPU count must be greater than 0")
	}
	if c.MemorySizeMiB == 0 {
		return fmt.Errorf("memory size must be greater than 0")
	}
	if c.VsockPort == 0 {
		c.VsockPort = 2222 // Default vsock port
	}

	for i, dir := range c.SharedDirs {
		if dir.Tag == "" {
			return fmt.Errorf("shared directory %d: tag is required", i)
		}
		if dir.HostPath == "" {
			return fmt.Errorf("shared directory %d: host path is required", i)
		}
	}

	return nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		CPUCount:      2,
		MemorySizeMiB: 2048,
		EnableNested:  true,
		VsockPort:     2222,
		BootArgs:      "console=hvc0 root=/dev/vda rw",
	}
}
