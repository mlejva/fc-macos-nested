package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newMachineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Configure and query machine settings",
	}

	cmd.AddCommand(newMachineConfigCmd())
	cmd.AddCommand(newMachineInfoCmd())
	cmd.AddCommand(newMachineVersionCmd())

	return cmd
}

func newMachineConfigCmd() *cobra.Command {
	var (
		vcpuCount       int
		memSizeMib      int
		smt             bool
		cpuTemplate     string
		trackDirtyPages bool
	)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configure the microVM machine settings",
		Example: `  # Set 2 vCPUs and 512 MiB memory
  fc-macos machine config --vcpus 2 --memory 512

  # Enable dirty page tracking for snapshots
  fc-macos machine config --vcpus 2 --memory 512 --track-dirty-pages`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := &api.MachineConfig{
				VCPUCount:       vcpuCount,
				MemSizeMib:      memSizeMib,
				SMT:             smt,
				CPUTemplate:     cpuTemplate,
				TrackDirtyPages: trackDirtyPages,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.SetMachineConfig(cmd.Context(), cfg); err != nil {
				return fmt.Errorf("failed to set machine config: %w", err)
			}

			fmt.Println("Machine configuration set successfully")
			return nil
		},
	}

	cmd.Flags().IntVar(&vcpuCount, "vcpus", 1, "number of vCPUs")
	cmd.Flags().IntVar(&memSizeMib, "memory", 128, "memory size in MiB")
	cmd.Flags().BoolVar(&smt, "smt", false, "enable SMT (simultaneous multithreading)")
	cmd.Flags().StringVar(&cpuTemplate, "cpu-template", "", "CPU template (C3, T2, T2S, T2CL, T2A, V1N1, None)")
	cmd.Flags().BoolVar(&trackDirtyPages, "track-dirty-pages", false, "enable dirty page tracking for snapshots")

	return cmd
}

func newMachineInfoCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Get the current machine configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetMachineConfig(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get machine config: %w", err)
			}

			cfg, err := convertTo[api.MachineConfig](result)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(cfg, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Printf("vCPUs: %d\n", cfg.VCPUCount)
				fmt.Printf("Memory: %d MiB\n", cfg.MemSizeMib)
				fmt.Printf("SMT: %v\n", cfg.SMT)
				if cfg.CPUTemplate != "" {
					fmt.Printf("CPU Template: %s\n", cfg.CPUTemplate)
				}
				fmt.Printf("Track Dirty Pages: %v\n", cfg.TrackDirtyPages)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}

func newMachineVersionCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Get Firecracker version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetVersion(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get version: %w", err)
			}

			version, err := convertTo[api.Version](result)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(version, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Printf("Firecracker Version: %s\n", version.FirecrackerVersion)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}
