package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newActionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "Control microVM lifecycle (start, stop, pause, resume)",
	}

	cmd.AddCommand(newActionsStartCmd())
	cmd.AddCommand(newActionsStopCmd())
	cmd.AddCommand(newActionsPauseCmd())
	cmd.AddCommand(newActionsResumeCmd())

	return cmd
}

func newActionsStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the microVM",
		Long: `Start the microVM instance. This requires that boot source and
at least a root drive have been configured.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.StartInstance(cmd.Context()); err != nil {
				return fmt.Errorf("failed to start instance: %w", err)
			}

			fmt.Println("MicroVM started successfully")
			return nil
		},
	}
}

func newActionsStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the microVM",
		Long: `Stop the microVM instance by sending Ctrl+Alt+Del to initiate
graceful shutdown. Use --force for immediate termination.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if force {
				if err := client.ForceStopInstance(cmd.Context()); err != nil {
					return fmt.Errorf("failed to force stop instance: %w", err)
				}
				fmt.Println("MicroVM force stopped")
			} else {
				if err := client.StopInstance(cmd.Context()); err != nil {
					return fmt.Errorf("failed to stop instance: %w", err)
				}
				fmt.Println("MicroVM stop signal sent")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force immediate termination")

	return cmd
}

func newActionsPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the microVM",
		Long:  `Pause the microVM execution. The VM state is preserved in memory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.PauseInstance(cmd.Context()); err != nil {
				return fmt.Errorf("failed to pause instance: %w", err)
			}

			fmt.Println("MicroVM paused")
			return nil
		},
	}
}

func newActionsResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume a paused microVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.ResumeInstance(cmd.Context()); err != nil {
				return fmt.Errorf("failed to resume instance: %w", err)
			}

			fmt.Println("MicroVM resumed")
			return nil
		},
	}
}
