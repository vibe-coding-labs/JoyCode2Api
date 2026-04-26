package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current authenticated user info",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := resolveClient()
		if err != nil {
			return err
		}
		resp, err := client.UserInfo()
		if err != nil {
			return err
		}
		data, _ := resp["data"].(map[string]interface{})
		fmt.Printf("  用户: %s\n", data["realName"])
		fmt.Printf("  ID: %s\n", data["userId"])
		fmt.Printf("  组织: %s\n", data["orgName"])
		fmt.Printf("  租户: %s\n", data["tenant"])
		status := "无效"
		if code, ok := resp["code"].(float64); ok && code == 0 {
			status = "有效"
		}
		fmt.Printf("  状态: %s\n", status)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
