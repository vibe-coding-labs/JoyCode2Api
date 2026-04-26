package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var (
	ptKey  string
	userID string
)

var rootCmd = &cobra.Command{
	Use:   "JoyCodeProxy",
	Short: "JoyCode OpenAI-Compatible API Proxy",
	Long:  "Convert JoyCode AI IDE APIs to OpenAI-compatible format for Codex and other tools.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&ptKey, "ptkey", "k", "", "JoyCode ptKey (auto-detected if empty)")
	rootCmd.PersistentFlags().StringVarP(&userID, "userid", "u", "", "JoyCode userID (auto-detected if empty)")
}

func resolveClient() (*joycode.Client, error) {
	var creds *auth.Credentials
	var err error
	if ptKey != "" && userID != "" {
		creds = &auth.Credentials{PtKey: ptKey, UserID: userID}
	} else {
		creds, err = auth.LoadFromSystem()
		if err != nil {
			return nil, fmt.Errorf("failed to load credentials: %w", err)
		}
	}
	return joycode.NewClient(creds.PtKey, creds.UserID), nil
}
