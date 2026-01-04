package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newBootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boot",
		Short: "Manage boot source configuration for the microVM",
	}

	cmd.AddCommand(newBootSetCmd())
	cmd.AddCommand(newBootGetCmd())

	return cmd
}

func newBootSetCmd() *cobra.Command {
	var (
		kernelPath string
		initrdPath string
		bootArgs   string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set the boot source for the microVM",
		Long: `Configure the kernel image, optional initrd, and boot arguments
for the Firecracker microVM.`,
		Example: `  # Set kernel with default boot args
  fc-macos boot set --kernel /path/to/vmlinux

  # Set kernel with custom boot args
  fc-macos boot set --kernel /path/to/vmlinux --boot-args "console=ttyS0 reboot=k panic=1"

  # Set kernel with initrd
  fc-macos boot set --kernel /path/to/vmlinux --initrd /path/to/initrd.img`,
		RunE: func(cmd *cobra.Command, args []string) error {
			bootSource := &api.BootSource{
				KernelImagePath: kernelPath,
				InitrdPath:      initrdPath,
				BootArgs:        bootArgs,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.SetBootSource(cmd.Context(), bootSource); err != nil {
				return fmt.Errorf("failed to set boot source: %w", err)
			}

			fmt.Println("Boot source configured successfully")
			return nil
		},
	}

	cmd.Flags().StringVar(&kernelPath, "kernel", "", "path to the kernel image (required)")
	cmd.Flags().StringVar(&initrdPath, "initrd", "", "path to the initrd image")
	cmd.Flags().StringVar(&bootArgs, "boot-args", "console=ttyS0 reboot=k panic=1 pci=off", "kernel boot arguments")
	cmd.MarkFlagRequired("kernel")

	return cmd
}

func newBootGetCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the current boot source configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetBootSource(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get boot source: %w", err)
			}

			bootSource, err := convertTo[api.BootSource](result)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(bootSource, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Printf("Kernel: %s\n", bootSource.KernelImagePath)
				if bootSource.InitrdPath != "" {
					fmt.Printf("Initrd: %s\n", bootSource.InitrdPath)
				}
				if bootSource.BootArgs != "" {
					fmt.Printf("Boot Args: %s\n", bootSource.BootArgs)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}
