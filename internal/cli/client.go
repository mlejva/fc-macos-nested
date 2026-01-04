package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// VMProvider interface allows lazy loading of the VM implementation
type VMProvider interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	State() string
	PingAgent() bool
	Logs(ctx context.Context, follow bool, lines int) error
	Shell(ctx context.Context) error
	Config() VMConfig
}

type VMConfig interface {
	GetCPUCount() uint
	GetMemorySizeMiB() uint64
}

// FirecrackerClientProvider interface for lazy loading
type FirecrackerClientProvider interface {
	SetBootSource(ctx context.Context, bs interface{}) error
	GetBootSource(ctx context.Context) (interface{}, error)
	SetDrive(ctx context.Context, id string, drive interface{}) error
	GetDrives(ctx context.Context) ([]interface{}, error)
	PatchDrive(ctx context.Context, id string, pathOnHost string) error
	DeleteDrive(ctx context.Context, id string) error
	SetNetworkInterface(ctx context.Context, id string, iface interface{}) error
	GetNetworkInterfaces(ctx context.Context) ([]interface{}, error)
	PatchNetworkInterface(ctx context.Context, id string, rxLimiter, txLimiter interface{}) error
	DeleteNetworkInterface(ctx context.Context, id string) error
	SetMachineConfig(ctx context.Context, cfg interface{}) error
	GetMachineConfig(ctx context.Context) (interface{}, error)
	GetVersion(ctx context.Context) (interface{}, error)
	StartInstance(ctx context.Context) error
	StopInstance(ctx context.Context) error
	ForceStopInstance(ctx context.Context) error
	PauseInstance(ctx context.Context) error
	ResumeInstance(ctx context.Context) error
	CreateSnapshot(ctx context.Context, params interface{}) error
	LoadSnapshot(ctx context.Context, params interface{}) error
	GetMetrics(ctx context.Context) (interface{}, error)
	SetBalloon(ctx context.Context, balloon interface{}) error
	GetBalloon(ctx context.Context) (interface{}, error)
	GetBalloonStats(ctx context.Context) (interface{}, error)
	PatchBalloon(ctx context.Context, amountMib int64) error
}

var (
	globalVM     VMProvider
	globalClient FirecrackerClientProvider
	globalVMMgr  *VMManager
	initOnce     sync.Once
	initErr      error

	// These will be set by the vmloader package
	vmInitializer func(ctx context.Context, cfg VMInitConfig) (VMProvider, FirecrackerClientProvider, error)
)

// VMInitConfig holds configuration for VM initialization
type VMInitConfig struct {
	KernelPath    string
	RootFSPath    string
	CPUCount      uint
	MemorySizeMiB uint64
	SharedDir     string
	VsockPort     uint32
}

// RegisterVMInitializer sets the function used to initialize the VM
// This should be called from a separate package that imports the heavy dependencies
func RegisterVMInitializer(fn func(ctx context.Context, cfg VMInitConfig) (VMProvider, FirecrackerClientProvider, error)) {
	vmInitializer = fn
}

// getFirecrackerClient returns a Firecracker API client.
// It initializes the Linux VM if not already running.
func getFirecrackerClient(cmd *cobra.Command) (FirecrackerClientProvider, error) {
	initOnce.Do(func() {
		initErr = initializeVM(cmd.Context())
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", initErr)
	}

	return globalClient, nil
}

// getVMManager returns the VM manager for direct VM operations.
func getVMManager(cmd *cobra.Command) (*VMManager, error) {
	initOnce.Do(func() {
		initErr = initializeVM(cmd.Context())
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", initErr)
	}

	return globalVMMgr, nil
}

func initializeVM(ctx context.Context) error {
	if vmInitializer == nil {
		return fmt.Errorf("VM support not available - binary was built without VM support")
	}

	kernelPath := viper.GetString("kernel")
	rootfsPath := viper.GetString("rootfs")
	cpus := viper.GetInt("cpus")
	memory := viper.GetInt("memory")
	sharedDir := viper.GetString("shared-dir")

	// Check required configuration
	if kernelPath == "" {
		return fmt.Errorf("kernel path not specified; use --kernel or set FC_MACOS_KERNEL")
	}
	if rootfsPath == "" {
		return fmt.Errorf("rootfs path not specified; use --rootfs or set FC_MACOS_ROOTFS")
	}

	logrus.Infof("Initializing Linux VM (kernel=%s, rootfs=%s, cpus=%d, memory=%dMiB)",
		kernelPath, rootfsPath, cpus, memory)

	cfg := VMInitConfig{
		KernelPath:    kernelPath,
		RootFSPath:    rootfsPath,
		CPUCount:      uint(cpus),
		MemorySizeMiB: uint64(memory),
		SharedDir:     sharedDir,
		VsockPort:     2222,
	}

	vm, client, err := vmInitializer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize VM: %w", err)
	}

	globalVM = vm
	globalClient = client
	globalVMMgr = &VMManager{vm: vm}

	// Wait for agent to be ready
	logrus.Info("Waiting for agent to be ready...")
	if err := waitForAgent(ctx, vm); err != nil {
		return fmt.Errorf("agent not ready: %w", err)
	}

	logrus.Info("Linux VM ready")
	return nil
}

func waitForAgent(ctx context.Context, vm VMProvider) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for agent")
		case <-ticker.C:
			if vm.PingAgent() {
				return nil
			}
		}
	}
}

// VMManager provides operations for managing the Linux VM directly.
type VMManager struct {
	vm VMProvider
}

// VMStatus represents the current state of the Linux VM.
type VMStatus struct {
	State          string
	Running        bool
	PID            int
	Uptime         time.Duration
	MemoryUsedMiB  int
	MemoryTotalMiB int
	CPUs           int
}

// Status returns the current VM status.
func (m *VMManager) Status(ctx context.Context) (*VMStatus, error) {
	if m.vm == nil {
		return &VMStatus{State: "not initialized"}, nil
	}

	state := m.vm.State()
	cfg := m.vm.Config()
	return &VMStatus{
		State:          state,
		Running:        state == "running",
		CPUs:           int(cfg.GetCPUCount()),
		MemoryTotalMiB: int(cfg.GetMemorySizeMiB()),
	}, nil
}

// Start starts the Linux VM.
func (m *VMManager) Start(ctx context.Context) error {
	if m.vm == nil {
		return fmt.Errorf("VM not initialized")
	}
	return m.vm.Start(ctx)
}

// Stop stops the Linux VM.
func (m *VMManager) Stop(ctx context.Context, force bool) error {
	if m.vm == nil {
		return fmt.Errorf("VM not initialized")
	}
	return m.vm.Stop(ctx, force)
}

// Logs streams logs from the VM.
func (m *VMManager) Logs(ctx context.Context, follow bool, lines int) error {
	if m.vm == nil {
		return fmt.Errorf("VM not initialized")
	}
	return m.vm.Logs(ctx, follow, lines)
}

// Shell opens an interactive shell to the VM.
func (m *VMManager) Shell(ctx context.Context) error {
	if m.vm == nil {
		return fmt.Errorf("VM not initialized")
	}
	return m.vm.Shell(ctx)
}
