package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newBalloonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balloon",
		Short: "Manage memory balloon device",
		Long:  `Control the virtio-balloon device for dynamic memory management.`,
	}

	cmd.AddCommand(newBalloonSetCmd())
	cmd.AddCommand(newBalloonGetCmd())
	cmd.AddCommand(newBalloonStatsCmd())
	cmd.AddCommand(newBalloonUpdateCmd())

	return cmd
}

func newBalloonSetCmd() *cobra.Command {
	var (
		amountMib             int64
		deflateOnOom          bool
		statsPollingIntervalS int64
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Configure the memory balloon device",
		Example: `  # Set balloon target to 256 MiB
  fc-macos balloon set --amount 256

  # Enable deflate on OOM
  fc-macos balloon set --amount 256 --deflate-on-oom

  # Enable statistics polling every 5 seconds
  fc-macos balloon set --amount 256 --stats-interval 5`,
		RunE: func(cmd *cobra.Command, args []string) error {
			balloon := &api.Balloon{
				AmountMib:             amountMib,
				DeflateOnOom:          deflateOnOom,
				StatsPollingIntervalS: statsPollingIntervalS,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.SetBalloon(cmd.Context(), balloon); err != nil {
				return fmt.Errorf("failed to set balloon: %w", err)
			}

			fmt.Println("Balloon configured successfully")
			return nil
		},
	}

	cmd.Flags().Int64Var(&amountMib, "amount", 0, "target balloon size in MiB (required)")
	cmd.Flags().BoolVar(&deflateOnOom, "deflate-on-oom", false, "deflate balloon on guest OOM")
	cmd.Flags().Int64Var(&statsPollingIntervalS, "stats-interval", 0, "stats polling interval in seconds")
	cmd.MarkFlagRequired("amount")

	return cmd
}

func newBalloonGetCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the current balloon configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetBalloon(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get balloon: %w", err)
			}

			balloon, err := convertTo[api.Balloon](result)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(balloon, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Printf("Target Amount: %d MiB\n", balloon.AmountMib)
				fmt.Printf("Deflate on OOM: %v\n", balloon.DeflateOnOom)
				if balloon.StatsPollingIntervalS > 0 {
					fmt.Printf("Stats Polling Interval: %d s\n", balloon.StatsPollingIntervalS)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}

func newBalloonStatsCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Get balloon statistics",
		Long:  `Get detailed memory statistics from the balloon device.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetBalloonStats(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get balloon stats: %w", err)
			}

			stats, err := convertTo[api.BalloonStats](result)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(stats, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Printf("Target Pages: %d\n", stats.TargetPages)
				fmt.Printf("Actual Pages: %d\n", stats.ActualPages)
				fmt.Printf("Target Memory: %d MiB\n", stats.TargetMib)
				fmt.Printf("Actual Memory: %d MiB\n", stats.ActualMib)
				if stats.FreeMemory > 0 {
					fmt.Printf("Free Memory: %d bytes\n", stats.FreeMemory)
				}
				if stats.TotalMemory > 0 {
					fmt.Printf("Total Memory: %d bytes\n", stats.TotalMemory)
				}
				if stats.AvailableMemory > 0 {
					fmt.Printf("Available Memory: %d bytes\n", stats.AvailableMemory)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}

func newBalloonUpdateCmd() *cobra.Command {
	var amountMib int64

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the balloon target size",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.PatchBalloon(cmd.Context(), amountMib); err != nil {
				return fmt.Errorf("failed to update balloon: %w", err)
			}

			fmt.Printf("Balloon target updated to %d MiB\n", amountMib)
			return nil
		},
	}

	cmd.Flags().Int64Var(&amountMib, "amount", 0, "new target balloon size in MiB (required)")
	cmd.MarkFlagRequired("amount")

	return cmd
}
