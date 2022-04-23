package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var rootCmd = &cobra.Command{
	Use:   "summary",
	Short: "image show chart images summary",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		chart := args[0]
		opt := imageOptions{
			chart: chart,
		}
		opt.runHelm3()
	},
}

//func init() {
//	rootCmd.AddCommand(imageCmd)
//	rootCmd.AddCommand(versionCmd)
//}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
