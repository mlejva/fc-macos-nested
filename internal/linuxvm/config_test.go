package linuxvm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 1024,
			},
			wantErr: false,
		},
		{
			name: "missing kernel path",
			config: &Config{
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 1024,
			},
			wantErr: true,
			errMsg:  "kernel path is required",
		},
		{
			name: "missing rootfs path",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				CPUCount:      2,
				MemorySizeMiB: 1024,
			},
			wantErr: true,
			errMsg:  "rootfs path is required",
		},
		{
			name: "zero CPU count",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      0,
				MemorySizeMiB: 1024,
			},
			wantErr: true,
			errMsg:  "CPU count must be greater than 0",
		},
		{
			name: "zero memory",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 0,
			},
			wantErr: true,
			errMsg:  "memory size must be greater than 0",
		},
		{
			name: "shared dir without tag",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 1024,
				SharedDirs: []SharedDir{
					{HostPath: "/shared"},
				},
			},
			wantErr: true,
			errMsg:  "tag is required",
		},
		{
			name: "shared dir without host path",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 1024,
				SharedDirs: []SharedDir{
					{Tag: "shared"},
				},
			},
			wantErr: true,
			errMsg:  "host path is required",
		},
		{
			name: "valid config with shared dirs",
			config: &Config{
				KernelPath:    "/path/to/kernel",
				RootFSPath:    "/path/to/rootfs",
				CPUCount:      2,
				MemorySizeMiB: 1024,
				SharedDirs: []SharedDir{
					{Tag: "shared", HostPath: "/shared"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, uint(2), cfg.CPUCount)
	assert.Equal(t, uint64(2048), cfg.MemorySizeMiB)
	assert.True(t, cfg.EnableNested)
	assert.Equal(t, uint32(2222), cfg.VsockPort)
	assert.NotEmpty(t, cfg.BootArgs)
}

func TestConfigValidateSetsDefaultVsockPort(t *testing.T) {
	cfg := &Config{
		KernelPath:    "/path/to/kernel",
		RootFSPath:    "/path/to/rootfs",
		CPUCount:      2,
		MemorySizeMiB: 1024,
		VsockPort:     0,
	}

	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, uint32(2222), cfg.VsockPort)
}
