package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "regrada",
	Short: "Regrada - CI for AI systems",
	Long: `Regrada is a CI tool for AI systems that detects behavioral
regressions in LLM-powered apps before they hit production.

Available commands:
  init  - Initialize a new regrada project
  trace - Run evaluations and capture traces
  diff  - Compare evaluation results
  gate  - Manage quality gates`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help()
			return
		}
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
}

// exitWithError prints an error message and exits
func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
