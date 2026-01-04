package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "network",
		Aliases: []string{"net"},
		Short:   "Manage network interfaces for the microVM",
	}

	cmd.AddCommand(newNetworkAddCmd())
	cmd.AddCommand(newNetworkListCmd())
	cmd.AddCommand(newNetworkUpdateCmd())
	cmd.AddCommand(newNetworkRemoveCmd())

	return cmd
}

func newNetworkAddCmd() *cobra.Command {
	var (
		ifaceID     string
		hostDevName string
		guestMAC    string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a network interface to the microVM",
		Example: `  # Add network interface with tap device
  fc-macos network add --id eth0 --tap tap0

  # Add network interface with custom MAC
  fc-macos network add --id eth0 --tap tap0 --mac 06:00:AC:10:00:02`,
		RunE: func(cmd *cobra.Command, args []string) error {
			iface := &api.NetworkInterface{
				IfaceID:     ifaceID,
				HostDevName: hostDevName,
				GuestMAC:    guestMAC,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.SetNetworkInterface(cmd.Context(), ifaceID, iface); err != nil {
				return fmt.Errorf("failed to add network interface: %w", err)
			}

			fmt.Printf("Network interface '%s' added successfully\n", ifaceID)
			return nil
		},
	}

	cmd.Flags().StringVar(&ifaceID, "id", "", "unique identifier for the interface (required)")
	cmd.Flags().StringVar(&hostDevName, "tap", "", "name of the TAP device on host (required)")
	cmd.Flags().StringVar(&guestMAC, "mac", "", "MAC address for the guest interface")
	cmd.MarkFlagRequired("id")
	cmd.MarkFlagRequired("tap")

	return cmd
}

func newNetworkListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured network interfaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetNetworkInterfaces(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get network interfaces: %w", err)
			}

			// Convert []interface{} to []api.NetworkInterface via JSON
			var interfaces []api.NetworkInterface
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &interfaces); err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(interfaces, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				if len(interfaces) == 0 {
					fmt.Println("No network interfaces configured")
					return nil
				}
				for _, iface := range interfaces {
					fmt.Printf("ID: %s\n", iface.IfaceID)
					fmt.Printf("  Host Device: %s\n", iface.HostDevName)
					if iface.GuestMAC != "" {
						fmt.Printf("  Guest MAC: %s\n", iface.GuestMAC)
					}
					fmt.Println()
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}

func newNetworkUpdateCmd() *cobra.Command {
	var (
		ifaceID string
		rxBw    int64
		txBw    int64
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update network interface rate limits",
		RunE: func(cmd *cobra.Command, args []string) error {
			var rxLimiter, txLimiter *api.RateLimiter

			if rxBw > 0 {
				rxLimiter = &api.RateLimiter{
					Bandwidth: &api.TokenBucket{
						Size:       rxBw,
						RefillTime: 1000, // 1 second
					},
				}
			}

			if txBw > 0 {
				txLimiter = &api.RateLimiter{
					Bandwidth: &api.TokenBucket{
						Size:       txBw,
						RefillTime: 1000,
					},
				}
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.PatchNetworkInterface(cmd.Context(), ifaceID, rxLimiter, txLimiter); err != nil {
				return fmt.Errorf("failed to update network interface: %w", err)
			}

			fmt.Printf("Network interface '%s' updated successfully\n", ifaceID)
			return nil
		},
	}

	cmd.Flags().StringVar(&ifaceID, "id", "", "interface identifier (required)")
	cmd.Flags().Int64Var(&rxBw, "rx-bandwidth", 0, "RX bandwidth limit in bytes/sec")
	cmd.Flags().Int64Var(&txBw, "tx-bandwidth", 0, "TX bandwidth limit in bytes/sec")
	cmd.MarkFlagRequired("id")

	return cmd
}

func newNetworkRemoveCmd() *cobra.Command {
	var ifaceID string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a network interface from the microVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.DeleteNetworkInterface(cmd.Context(), ifaceID); err != nil {
				return fmt.Errorf("failed to remove network interface: %w", err)
			}

			fmt.Printf("Network interface '%s' removed successfully\n", ifaceID)
			return nil
		},
	}

	cmd.Flags().StringVar(&ifaceID, "id", "", "interface identifier (required)")
	cmd.MarkFlagRequired("id")

	return cmd
}
