package cmd

import (
	"github.com/spf13/cobra"
)

// initCmd 初始化容器命令
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize container process to run user's process in container",
	RunE: func(_ *cobra.Command, args []string) error {
		return RunContainerInit()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func RunContainerInit() error {
	return nil
}
