package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

var resetPasswordCmd = &cobra.Command{
	Use:     "reset-password",
	Short:   "重置 Dashboard root 密码",
	Long:    "重置 Dashboard 管理界面的 root 用户密码。如果忘记密码，可以用这个命令重新设置。",
	GroupID: "core",
	Example: `  # 交互式重置密码
  joycode-proxy reset-password

  # 直接指定新密码
  joycode-proxy reset-password -p my_new_password`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open("")
		if err != nil {
			return fmt.Errorf("打开数据库失败: %w", err)
		}
		defer s.Close()

		newPw, _ := cmd.Flags().GetString("new-password")
		if newPw == "" {
			fmt.Print("请输入新密码（至少 6 位）: ")
			fmt.Scanln(&newPw)
		}

		if len(newPw) < 6 {
			return fmt.Errorf("密码长度不能少于 6 位")
		}

		hash, err := auth.HashPassword(newPw)
		if err != nil {
			return fmt.Errorf("密码加密失败: %w", err)
		}

		if err := s.SetSetting("auth_password_hash", hash); err != nil {
			return fmt.Errorf("保存密码失败: %w", err)
		}

		if s.GetSetting("auth_jwt_secret") == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("生成 JWT secret 失败: %w", err)
			}
			if err := s.SetSetting("auth_jwt_secret", hex.EncodeToString(b)); err != nil {
				return fmt.Errorf("保存 JWT secret 失败: %w", err)
			}
		}

		fmt.Println("密码重置成功")
		return nil
	},
}

func init() {
	resetPasswordCmd.Flags().StringP("new-password", "p", "", "新密码")
	rootCmd.AddCommand(resetPasswordCmd)
}
