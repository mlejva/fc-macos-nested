package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Get microVM metrics",
	}

	cmd.AddCommand(newMetricsGetCmd())

	return cmd
}

func newMetricsGetCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get current metrics from the microVM",
		Long:  `Retrieve performance metrics including CPU, memory, network, and block device statistics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			metrics, err := client.GetMetrics(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get metrics: %w", err)
			}

			if outputJSON {
				data, err := json.MarshalIndent(metrics, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				// Print a summary of key metrics
				data, err := json.MarshalIndent(metrics, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", true, "output in JSON format")

	return cmd
}
