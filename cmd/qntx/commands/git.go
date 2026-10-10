package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	errors "github.com/teranos/sacred-error"
)

// What git asks of the node (ADR-048, Its git). An agent holds no GitHub
// token: its git is configured with this as its credential helper, and the
// node mints what carries the push when git asks.

// GitCmd is the git verb.
var GitCmd = &cobra.Command{
	Use:   "git",
	Short: "What git asks of the node",
}

var gitCredentialCmd = &cobra.Command{
	Use:   "credential OPERATION",
	Short: "git's credential helper: the node mints what carries a push",
	Long: `A git credential helper (gitcredentials(7)). git runs it with get, store or
erase and says on stdin what it is about to reach. For a repository on
github.com the node is asked for a credential and git is handed it; nothing
is stored, so store and erase do nothing.

git has to say which repository, which it does when credential.useHttpPath
is true:

  [credential "https://github.com"]
      helper = !qntx git credential --to http://127.0.0.1:8770
      useHttpPath = true`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return gitCredential(args[0], os.Stdin, os.Stdout, mintedByTheNode)
	},
}

func init() {
	gitCredentialCmd.Flags().StringVar(&elementTo, "to", "", "Node to reach (default: this machine's, from am.toml)")
	gitCredentialCmd.Flags().StringVar(&elementToken, "token", "", "Bearer token (default: $QNTX_TOKEN, then ~/.qntx/token)")
	GitCmd.AddCommand(gitCredentialCmd)
}

// gitHubHost is the one host the node mints a credential for.
const gitHubHost = "github.com"

// gitCredential answers git as its credential helper. mint asks the node for
// what carries a push to one repository.
func gitCredential(operation string, asked io.Reader, answer io.Writer, mint func(owner, repo string) (username, password string, err error)) error {
	// Nothing is kept, so there is nothing to store and nothing to erase.
	if operation != "get" {
		return nil
	}
	about := map[string]string{}
	lines := bufio.NewScanner(asked)
	for lines.Scan() {
		key, value, said := strings.Cut(lines.Text(), "=")
		if said {
			about[key] = value
		}
	}
	if err := lines.Err(); err != nil {
		return errors.Wrap(err, "what git asked did not read")
	}
	// Another host is left to whatever else git has for it.
	if about["host"] != gitHubHost {
		return nil
	}
	owner, repo, named := strings.Cut(strings.Trim(about["path"], "/"), "/")
	repo = strings.TrimSuffix(repo, ".git")
	if !named || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return errors.Newf("git did not say which repository on %s it is reaching (path %q): set credential.useHttpPath to true",
			gitHubHost, about["path"])
	}
	username, password, err := mint(owner, repo)
	if err != nil {
		return errors.Wrapf(err, "the node minted no credential for %s/%s", owner, repo)
	}
	_, err = fmt.Fprintf(answer, "username=%s\npassword=%s\n", username, password)
	return errors.Wrap(err, "the credential was not handed to git")
}

// mintedByTheNode asks the node for the credential (github credential).
func mintedByTheNode(owner, repo string) (string, string, error) {
	body, err := json.Marshal(map[string]string{"owner": owner, "repo": repo})
	if err != nil {
		return "", "", errors.Wrap(err, "the repository asked about did not marshal")
	}
	answered, err := ask("POST", nodeURL()+"/api/github/credential", body)
	if err != nil {
		return "", "", err
	}
	var minted struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Contents string `json:"contents"`
	}
	if err := json.Unmarshal(answered, &minted); err != nil {
		return "", "", errors.Wrap(err, "what the node answered is not a credential")
	}
	if minted.Password == "" {
		return "", "", errors.New("the node answered a credential with no token in it")
	}
	// git prints a helper's stderr: a push this cannot carry says so before GitHub refuses it.
	if minted.Contents != "write" {
		fmt.Fprintf(os.Stderr, "qntx: this credential may %q the contents of %s/%s, so a push with it is refused by GitHub\n", minted.Contents, owner, repo)
	}
	return minted.Username, minted.Password, nil
}
