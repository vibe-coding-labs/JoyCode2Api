package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var Version = "0.1.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("JoyCodeProxy %s (JoyCode API %s)\n", Version, joycode.ClientVersion)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
