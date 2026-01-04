// Package cli implements the fc-macos command-line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// NewRootCmd creates the root command for fc-macos CLI.
func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "fc-macos",
		Short: "Run Firecracker microVMs on macOS",
		Long: `fc-macos is a CLI tool that runs Firecracker microVMs on macOS
using Apple's Virtualization Framework with nested virtualization.

It creates a Linux VM with KVM support, and runs Firecracker inside it
to provide the full Firecracker API on macOS.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		SilenceUsage: true,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.fc-macos.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("kernel", "", "path to Linux kernel for the intermediate VM")
	rootCmd.PersistentFlags().String("rootfs", "", "path to rootfs for the intermediate VM")
	rootCmd.PersistentFlags().Int("cpus", 2, "number of CPUs for the intermediate VM")
	rootCmd.PersistentFlags().Int("memory", 2048, "memory in MiB for the intermediate VM")
	rootCmd.PersistentFlags().String("shared-dir", "", "directory to share with the VM via virtio-fs")

	// Bind flags to viper
	viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("kernel", rootCmd.PersistentFlags().Lookup("kernel"))
	viper.BindPFlag("rootfs", rootCmd.PersistentFlags().Lookup("rootfs"))
	viper.BindPFlag("cpus", rootCmd.PersistentFlags().Lookup("cpus"))
	viper.BindPFlag("memory", rootCmd.PersistentFlags().Lookup("memory"))
	viper.BindPFlag("shared-dir", rootCmd.PersistentFlags().Lookup("shared-dir"))

	// Add subcommands
	rootCmd.AddCommand(newVersionCmd(version))
	rootCmd.AddCommand(newSetupCmd())
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newMicroVMCmd())
	rootCmd.AddCommand(newBootCmd())
	rootCmd.AddCommand(newDrivesCmd())
	rootCmd.AddCommand(newNetworkCmd())
	rootCmd.AddCommand(newMachineCmd())
	rootCmd.AddCommand(newActionsCmd())
	rootCmd.AddCommand(newSnapshotsCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newBalloonCmd())
	rootCmd.AddCommand(newVMCmd())
	rootCmd.AddCommand(newDashboardCmd())

	return rootCmd
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".fc-macos")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("FC_MACOS")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Configure logging based on log level
	level, err := logrus.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	return nil
}
