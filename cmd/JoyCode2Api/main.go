package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func main() {
	// Check if we're being invoked as daemon supervisor
	if os.Getenv("_JOYCODE_DAEMON_SUPERVISOR") == "1" {
		port, _ := strconv.Atoi(os.Getenv("_JOYCODE_DAEMON_PORT"))
		if port == 0 {
			port = 34891
		}
		RunSupervisor(port)
		return
	}

	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "service", Title: "Service Management:"},
		&cobra.Group{ID: "query", Title: "Query & Info:"},
	)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
