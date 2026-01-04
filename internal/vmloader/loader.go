// Package vmloader registers the VM initializer that loads the heavy
// virtualization framework dependencies. Import this package in main.go
// to enable VM functionality.
package vmloader

import (
	"context"
	"fmt"

	"github.com/anthropics/fc-macos/internal/cli"
	"github.com/anthropics/fc-macos/internal/linuxvm"
	"github.com/anthropics/fc-macos/internal/proxy"
)

func init() {
	cli.RegisterVMInitializer(initializeVM)
}

// vmWrapper implements cli.VMProvider
type vmWrapper struct {
	vm *linuxvm.VM
}

func (w *vmWrapper) Start(ctx context.Context) error {
	return w.vm.Start(ctx)
}

func (w *vmWrapper) Stop(ctx context.Context, force bool) error {
	return w.vm.Stop(ctx, force)
}

func (w *vmWrapper) State() string {
	return w.vm.State().String()
}

func (w *vmWrapper) PingAgent() bool {
	return w.vm.PingAgent()
}

func (w *vmWrapper) Logs(ctx context.Context, follow bool, lines int) error {
	return w.vm.Logs(ctx, follow, lines)
}

func (w *vmWrapper) Shell(ctx context.Context) error {
	return w.vm.Shell(ctx)
}

func (w *vmWrapper) Config() cli.VMConfig {
	return &configWrapper{cfg: w.vm.Config()}
}

// configWrapper implements cli.VMConfig
type configWrapper struct {
	cfg *linuxvm.Config
}

func (c *configWrapper) GetCPUCount() uint {
	return c.cfg.CPUCount
}

func (c *configWrapper) GetMemorySizeMiB() uint64 {
	return c.cfg.MemorySizeMiB
}

// clientWrapper implements cli.FirecrackerClientProvider
type clientWrapper struct {
	client *proxy.FirecrackerClient
}

func (c *clientWrapper) SetBootSource(ctx context.Context, bs interface{}) error {
	// Type assertion will be handled by the caller passing the correct type
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetBootSource(ctx context.Context) (interface{}, error) {
	return nil, fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) SetDrive(ctx context.Context, id string, drive interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetDrives(ctx context.Context) ([]interface{}, error) {
	return nil, fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) PatchDrive(ctx context.Context, id string, pathOnHost string) error {
	return c.client.PatchDrive(ctx, id, pathOnHost)
}

func (c *clientWrapper) DeleteDrive(ctx context.Context, id string) error {
	return c.client.DeleteDrive(ctx, id)
}

func (c *clientWrapper) SetNetworkInterface(ctx context.Context, id string, iface interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetNetworkInterfaces(ctx context.Context) ([]interface{}, error) {
	return nil, fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) PatchNetworkInterface(ctx context.Context, id string, rxLimiter, txLimiter interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) DeleteNetworkInterface(ctx context.Context, id string) error {
	return c.client.DeleteNetworkInterface(ctx, id)
}

func (c *clientWrapper) SetMachineConfig(ctx context.Context, cfg interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetMachineConfig(ctx context.Context) (interface{}, error) {
	return nil, fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetVersion(ctx context.Context) (interface{}, error) {
	return c.client.GetVersion(ctx)
}

func (c *clientWrapper) StartInstance(ctx context.Context) error {
	return c.client.StartInstance(ctx)
}

func (c *clientWrapper) StopInstance(ctx context.Context) error {
	return c.client.StopInstance(ctx)
}

func (c *clientWrapper) ForceStopInstance(ctx context.Context) error {
	return c.client.ForceStopInstance(ctx)
}

func (c *clientWrapper) PauseInstance(ctx context.Context) error {
	return c.client.PauseInstance(ctx)
}

func (c *clientWrapper) ResumeInstance(ctx context.Context) error {
	return c.client.ResumeInstance(ctx)
}

func (c *clientWrapper) CreateSnapshot(ctx context.Context, params interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) LoadSnapshot(ctx context.Context, params interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetMetrics(ctx context.Context) (interface{}, error) {
	return c.client.GetMetrics(ctx)
}

func (c *clientWrapper) SetBalloon(ctx context.Context, balloon interface{}) error {
	return fmt.Errorf("not implemented - use typed client directly")
}

func (c *clientWrapper) GetBalloon(ctx context.Context) (interface{}, error) {
	return c.client.GetBalloon(ctx)
}

func (c *clientWrapper) GetBalloonStats(ctx context.Context) (interface{}, error) {
	return c.client.GetBalloonStats(ctx)
}

func (c *clientWrapper) PatchBalloon(ctx context.Context, amountMib int64) error {
	return c.client.PatchBalloon(ctx, amountMib)
}

// GetTypedClient returns the underlying typed client for commands that need it
func (c *clientWrapper) GetTypedClient() *proxy.FirecrackerClient {
	return c.client
}

func initializeVM(ctx context.Context, cfg cli.VMInitConfig) (cli.VMProvider, cli.FirecrackerClientProvider, error) {
	vmCfg := &linuxvm.Config{
		KernelPath:    cfg.KernelPath,
		RootFSPath:    cfg.RootFSPath,
		CPUCount:      cfg.CPUCount,
		MemorySizeMiB: cfg.MemorySizeMiB,
		EnableNested:  true,
		VsockPort:     cfg.VsockPort,
	}

	if cfg.SharedDir != "" {
		vmCfg.SharedDirs = []linuxvm.SharedDir{
			{Tag: "shared", HostPath: cfg.SharedDir, ReadOnly: false},
		}
	}

	vm, err := linuxvm.New(vmCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create VM: %w", err)
	}

	if err := vm.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to start VM: %w", err)
	}

	transport := proxy.NewVsockTransport(vm.VsockDevice(), cfg.VsockPort)
	client := proxy.NewFirecrackerClient(transport)

	return &vmWrapper{vm: vm}, &clientWrapper{client: client}, nil
}
