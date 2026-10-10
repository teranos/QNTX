package server

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// The ROOT agent's git (ADR-048, Its git): who its commits are by, and how its
// git comes by what carries a push.

// gitIdentityOf is who an agent's commits are by: the ROOT agent of the node
// named, at the address its DID is at the host the node answers on.
func gitIdentityOf(did, nodeName, publicOrigin string) (name, email string) {
	name = "ROOT agent"
	if nodeName != "" {
		name = nodeName + " " + name
	}
	host := "localhost"
	if origin, err := url.Parse(publicOrigin); err == nil && origin.Hostname() != "" {
		host = origin.Hostname()
	}
	return name, strings.TrimPrefix(did, "did:key:") + "@" + host
}

// writeGitConfig writes the git configuration the agent's git reads as its
// global one: its identity, and the node as where a credential for GitHub is
// asked. qntx is this node's own binary, and node where it answers.
func (a *rootAgent) writeGitConfig(name, email, qntx, node string) (string, error) {
	if err := os.MkdirAll(a.home, 0o700); err != nil {
		return "", errors.Wrapf(err, "could not create %s", a.home)
	}
	// git runs a helper that starts with ! through the shell.
	helper := "!" + strconv.Quote(qntx) + " git credential --to " + strconv.Quote(node)
	config := "[user]\n" +
		"\tname = " + strconv.Quote(name) + "\n" +
		"\temail = " + strconv.Quote(email) + "\n" +
		"[credential \"https://github.com\"]\n" +
		"\thelper = " + strconv.Quote(helper) + "\n" +
		"\tuseHttpPath = true\n"
	path := filepath.Join(a.home, "gitconfig")
	return path, errors.Wrapf(os.WriteFile(path, []byte(config), 0o600), "could not write %s", path)
}

// gitEnvironment is what the agent's process is handed so that its git is its
// own: the configuration above, and the token it is to the node, which is what
// the credential helper presents.
func (s *QNTXServer) gitEnvironment(agent *rootAgent) ([]string, error) {
	qntx, err := os.Executable()
	if err != nil {
		return nil, errors.Wrap(err, "the node does not know where its own binary is, so git has no helper to call")
	}
	origin := ""
	if s.authHandler != nil {
		origin = s.authHandler.PublicOrigin()
	}
	name, email := gitIdentityOf(agent.did, s.deps.cfg.Node.Name, origin)
	config, err := agent.writeGitConfig(name, email, qntx, s.ownURL)
	if err != nil {
		return nil, err
	}
	return []string{"GIT_CONFIG_GLOBAL=" + config, "QNTX_TOKEN=" + agent.token}, nil
}
