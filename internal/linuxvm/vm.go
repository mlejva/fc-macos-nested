package linuxvm

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/sirupsen/logrus"
)

// State represents the VM state.
type State int

const (
	// StateNotStarted indicates the VM has not been started.
	StateNotStarted State = iota
	// StateStarting indicates the VM is starting.
	StateStarting
	// StateRunning indicates the VM is running.
	StateRunning
	// StatePaused indicates the VM is paused.
	StatePaused
	// StateStopping indicates the VM is stopping.
	StateStopping
	// StateStopped indicates the VM has stopped.
	StateStopped
	// StateError indicates the VM encountered an error.
	StateError
)

func (s State) String() string {
	switch s {
	case StateNotStarted:
		return "not started"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// VM represents a Linux virtual machine.
type VM struct {
	config    *Config
	vm        *vz.VirtualMachine
	vmConfig  *vz.VirtualMachineConfiguration
	state     State
	mu        sync.RWMutex
	startTime time.Time

	// Console I/O
	consoleReader io.Reader
	consoleWriter io.Writer

	// vsock for communication
	vsockDevice *vz.VirtioSocketDevice
}

// New creates a new Linux VM with the given configuration.
func New(cfg *Config) (*VM, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	vm := &VM{
		config: cfg,
		state:  StateNotStarted,
	}

	if err := vm.createVMConfiguration(); err != nil {
		return nil, fmt.Errorf("failed to create VM configuration: %w", err)
	}

	return vm, nil
}

// Config returns the VM configuration.
func (v *VM) Config() *Config {
	return v.config
}

// State returns the current VM state.
func (v *VM) State() State {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.state
}

// VsockDevice returns the vsock device for communication.
func (v *VM) VsockDevice() *vz.VirtioSocketDevice {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.vsockDevice
}

// Start starts the VM.
func (v *VM) Start(ctx context.Context) error {
	v.mu.Lock()
	if v.state != StateNotStarted && v.state != StateStopped {
		v.mu.Unlock()
		return fmt.Errorf("VM cannot be started in state: %s", v.state)
	}
	v.state = StateStarting
	v.mu.Unlock()

	logrus.Info("Creating virtual machine instance")

	vm, err := vz.NewVirtualMachine(v.vmConfig)
	if err != nil {
		v.setState(StateError)
		return fmt.Errorf("failed to create VM instance: %w", err)
	}
	v.vm = vm

	// Get vsock device
	socketDevices := vm.SocketDevices()
	if len(socketDevices) > 0 {
		v.mu.Lock()
		v.vsockDevice = socketDevices[0]
		v.mu.Unlock()
	}

	// Start the VM
	logrus.Info("Starting virtual machine")
	if err := vm.Start(); err != nil {
		v.setState(StateError)
		return fmt.Errorf("failed to start VM: %w", err)
	}

	v.mu.Lock()
	v.state = StateRunning
	v.startTime = time.Now()
	v.mu.Unlock()

	logrus.Info("Virtual machine started successfully")
	return nil
}

// Stop stops the VM.
func (v *VM) Stop(ctx context.Context, force bool) error {
	v.mu.Lock()
	if v.state != StateRunning && v.state != StatePaused {
		v.mu.Unlock()
		return fmt.Errorf("VM cannot be stopped in state: %s", v.state)
	}
	v.state = StateStopping
	v.mu.Unlock()

	logrus.Info("Stopping virtual machine")

	if force {
		if err := v.vm.Stop(); err != nil {
			v.setState(StateError)
			return fmt.Errorf("failed to force stop VM: %w", err)
		}
	} else {
		if canRequest, err := v.vm.RequestStop(); err != nil {
			v.setState(StateError)
			return fmt.Errorf("failed to request VM stop: %w", err)
		} else if !canRequest {
			// Fall back to force stop
			if err := v.vm.Stop(); err != nil {
				v.setState(StateError)
				return fmt.Errorf("failed to stop VM: %w", err)
			}
		}
	}

	v.setState(StateStopped)
	logrus.Info("Virtual machine stopped")
	return nil
}

// Pause pauses the VM.
func (v *VM) Pause(ctx context.Context) error {
	v.mu.Lock()
	if v.state != StateRunning {
		v.mu.Unlock()
		return fmt.Errorf("VM cannot be paused in state: %s", v.state)
	}
	v.mu.Unlock()

	if err := v.vm.Pause(); err != nil {
		return fmt.Errorf("failed to pause VM: %w", err)
	}

	v.setState(StatePaused)
	logrus.Info("Virtual machine paused")
	return nil
}

// Resume resumes a paused VM.
func (v *VM) Resume(ctx context.Context) error {
	v.mu.Lock()
	if v.state != StatePaused {
		v.mu.Unlock()
		return fmt.Errorf("VM cannot be resumed in state: %s", v.state)
	}
	v.mu.Unlock()

	if err := v.vm.Resume(); err != nil {
		return fmt.Errorf("failed to resume VM: %w", err)
	}

	v.setState(StateRunning)
	logrus.Info("Virtual machine resumed")
	return nil
}

// PingAgent checks if the agent is responding.
func (v *VM) PingAgent() bool {
	v.mu.RLock()
	device := v.vsockDevice
	port := v.config.VsockPort
	v.mu.RUnlock()

	if device == nil {
		return false
	}

	conn, err := device.Connect(uint32(port))
	if err != nil {
		return false
	}
	defer conn.Close()

	// Set short timeout
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send ping
	if _, err := conn.Write([]byte("PING\n")); err != nil {
		return false
	}

	// Read response
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return false
	}

	return string(buf[:n]) == "PONG\n"
}

// Logs streams logs from the VM console.
func (v *VM) Logs(ctx context.Context, follow bool, lines int) error {
	// TODO: Implement console log streaming
	return fmt.Errorf("not implemented")
}

// Shell opens an interactive shell to the VM.
func (v *VM) Shell(ctx context.Context) error {
	// TODO: Implement interactive shell via vsock
	return fmt.Errorf("not implemented")
}

func (v *VM) setState(state State) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = state
}

func (v *VM) createVMConfiguration() error {
	logrus.Debugf("Creating VM configuration: cpus=%d, memory=%dMiB, nested=%v",
		v.config.CPUCount, v.config.MemorySizeMiB, v.config.EnableNested)

	// Create bootloader
	bootArgs := v.config.BootArgs
	if bootArgs == "" {
		bootArgs = "console=hvc0 root=/dev/vda rw"
	}

	bootloaderOpts := []vz.LinuxBootLoaderOption{
		vz.WithCommandLine(bootArgs),
	}
	if v.config.InitrdPath != "" {
		bootloaderOpts = append(bootloaderOpts, vz.WithInitrd(v.config.InitrdPath))
	}

	bootloader, err := vz.NewLinuxBootLoader(v.config.KernelPath, bootloaderOpts...)
	if err != nil {
		return fmt.Errorf("failed to create bootloader: %w", err)
	}

	// Create VM configuration
	vmConfig, err := vz.NewVirtualMachineConfiguration(
		bootloader,
		v.config.CPUCount,
		v.config.MemorySizeMiB*1024*1024, // Convert MiB to bytes
	)
	if err != nil {
		return fmt.Errorf("failed to create VM configuration: %w", err)
	}

	// Configure platform for nested virtualization
	if v.config.EnableNested {
		if err := v.configureNestedVirtualization(vmConfig); err != nil {
			return err
		}
	}

	// Configure devices
	if err := v.configureDevices(vmConfig); err != nil {
		return err
	}

	// Validate configuration
	validated, err := vmConfig.Validate()
	if err != nil {
		return fmt.Errorf("VM configuration validation failed: %w", err)
	}
	if !validated {
		return fmt.Errorf("VM configuration is invalid")
	}

	v.vmConfig = vmConfig
	return nil
}

func (v *VM) configureNestedVirtualization(vmConfig *vz.VirtualMachineConfiguration) error {
	// Check if nested virtualization is supported
	if !vz.IsNestedVirtualizationSupported() {
		return fmt.Errorf("nested virtualization is not supported on this system; requires M3+ chip and macOS 15+")
	}

	logrus.Debug("Nested virtualization is supported, enabling...")

	// Create generic platform configuration
	platformConfig, err := vz.NewGenericPlatformConfiguration()
	if err != nil {
		return fmt.Errorf("failed to create platform configuration: %w", err)
	}

	// Enable nested virtualization
	platformConfig.SetNestedVirtualizationEnabled(true)

	vmConfig.SetPlatformVirtualMachineConfiguration(platformConfig)
	logrus.Debug("Nested virtualization enabled")

	return nil
}

func (v *VM) configureDevices(vmConfig *vz.VirtualMachineConfiguration) error {
	var storageDevices []vz.StorageDeviceConfiguration
	var networkDevices []*vz.VirtioNetworkDeviceConfiguration
	var socketDevices []vz.SocketDeviceConfiguration
	var serialPorts []*vz.VirtioConsoleDeviceSerialPortConfiguration

	// Root filesystem block device
	logrus.Debugf("Configuring root filesystem: %s", v.config.RootFSPath)
	rootAttachment, err := vz.NewDiskImageStorageDeviceAttachment(v.config.RootFSPath, false)
	if err != nil {
		return fmt.Errorf("failed to create root disk attachment: %w", err)
	}
	rootDevice, err := vz.NewVirtioBlockDeviceConfiguration(rootAttachment)
	if err != nil {
		return fmt.Errorf("failed to create root block device: %w", err)
	}
	storageDevices = append(storageDevices, rootDevice)

	// Network device (NAT)
	logrus.Debug("Configuring NAT network device")
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return fmt.Errorf("failed to create NAT attachment: %w", err)
	}
	networkDevice, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		return fmt.Errorf("failed to create network device: %w", err)
	}
	networkDevices = append(networkDevices, networkDevice)

	// Vsock device for host-guest communication
	logrus.Debugf("Configuring vsock device on port %d", v.config.VsockPort)
	vsockDevice, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return fmt.Errorf("failed to create vsock device: %w", err)
	}
	socketDevices = append(socketDevices, vsockDevice)

	// Serial console
	logrus.Debug("Configuring serial console")
	serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create serial port attachment: %w", err)
	}
	consoleDevice, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)
	if err != nil {
		return fmt.Errorf("failed to create console device: %w", err)
	}
	serialPorts = append(serialPorts, consoleDevice)

	// Entropy device
	logrus.Debug("Configuring entropy device")
	entropyDevice, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return fmt.Errorf("failed to create entropy device: %w", err)
	}

	// Shared directories via virtio-fs
	var fsDevices []vz.DirectorySharingDeviceConfiguration
	for _, dir := range v.config.SharedDirs {
		logrus.Debugf("Configuring shared directory: tag=%s, path=%s", dir.Tag, dir.HostPath)
		fsDevice, err := v.createSharedDirectoryDevice(dir)
		if err != nil {
			return fmt.Errorf("failed to create shared directory device: %w", err)
		}
		fsDevices = append(fsDevices, fsDevice)
	}

	// Apply configurations
	vmConfig.SetStorageDevicesVirtualMachineConfiguration(storageDevices)
	vmConfig.SetNetworkDevicesVirtualMachineConfiguration(networkDevices)
	vmConfig.SetSocketDevicesVirtualMachineConfiguration(socketDevices)
	vmConfig.SetSerialPortsVirtualMachineConfiguration(serialPorts)
	vmConfig.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropyDevice})

	if len(fsDevices) > 0 {
		vmConfig.SetDirectorySharingDevicesVirtualMachineConfiguration(fsDevices)
	}

	return nil
}

func (v *VM) createSharedDirectoryDevice(dir SharedDir) (*vz.VirtioFileSystemDeviceConfiguration, error) {
	sharedDirectory, err := vz.NewSharedDirectory(dir.HostPath, dir.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared directory: %w", err)
	}

	directoryShare, err := vz.NewSingleDirectoryShare(sharedDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory share: %w", err)
	}

	fsConfig, err := vz.NewVirtioFileSystemDeviceConfiguration(dir.Tag)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem device configuration: %w", err)
	}
	fsConfig.SetDirectoryShare(directoryShare)

	return fsConfig, nil
}
