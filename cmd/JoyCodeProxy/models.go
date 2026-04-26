package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available AI models",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := resolveClient()
		if err != nil {
			return err
		}
		models, err := client.ListModels()
		if err != nil {
			return err
		}
		for _, m := range models {
			pref := ""
			if m.ChatAPIModel == joycode.DefaultModel {
				pref = " *"
			}
			fmt.Printf("  %s (%s) ctx=%d out=%d%s\n",
				m.Label, m.ChatAPIModel, m.MaxTotalTokens, m.RespMaxTokens, pref)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(modelsCmd)
}
