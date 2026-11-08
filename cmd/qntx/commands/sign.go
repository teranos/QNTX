package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// "find a way to sign the request?"
//
// "A signature proves the key, and nothing reusable leaves the node."

// SignCmd prints the Authorization one request to a node goes with: a JWT the
// key kept in ~/.qntx/key signs for that node, spent once there.
var SignCmd = &cobra.Command{
	Use:   "sign URL",
	Short: "What one request to a node is signed with, for curl -H",
	Long: `Print the Authorization value for one request to the node at URL, signed by
the key kept in ~/.qntx/key and made out to the node's origin. The node spends
it once, so each request asks for its own:

  curl -X POST $NODE/api/agent/say -H "Authorization: $(qntx sign $NODE)" \
    -H 'Content-Type: application/json' -d '{"says":"…","harness":"claude"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		signed, err := signedFor(args[0])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), signed)
		return err
	},
}
