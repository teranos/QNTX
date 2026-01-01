package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/internal/version"
)

// VersionCmd represents the version command
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show QNTX version information",
	Long:  `Display version, build time, commit hash, and platform information for the QNTX binary.`,
	Run: func(cmd *cobra.Command, args []string) {
		jsonOutput, _ := cmd.Flags().GetBool("json")

		info := version.Get()

		if jsonOutput {
			output, err := json.MarshalIndent(info, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error formatting JSON: %v\n", err)
				return
			}
			fmt.Println(string(output))
		} else {
			fmt.Println(info.String())
			fmt.Printf("Platform: %s\n", info.Platform)
			fmt.Printf("Go: %s\n", info.GoVersion)
		}
	},
}

func init() {
	VersionCmd.Flags().BoolP("json", "j", false, "Output version info as JSON")
}
