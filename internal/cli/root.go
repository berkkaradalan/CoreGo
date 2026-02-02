package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "corego",
	Short: "CoreGo CLI - Scaffold Go backend projects",
	Long:  `CoreGo CLI helps you create Go backend projects with database support, authentication, and more.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}