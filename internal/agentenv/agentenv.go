// Package agentenv is the environment an agent's harness starts with.
//
// "every call it makes carries its own credential. The credential of whoever
// sent the work never reaches what the agent calls" (ADR-048).
//
// The node's own environment holds what am.toml resolves as env: references,
// so a harness handed all of it would carry the node's credentials beside its
// own. It starts from what a process needs to run on the box and nothing else;
// what is the agent's own is added by whoever runs it.
package agentenv

import "os"

// kept is what a process needs to find its tools, write its files and reach
// out over TLS. None of it is a credential.
var kept = []string{
	"PATH",
	"HOME",
	"TMPDIR",
	"LANG",
	"LC_ALL",
	"TZ",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
	"NIX_SSL_CERT_FILE",
}

// Carried is the node's values for what is kept, and only those it has.
func Carried() []string {
	var env []string
	for _, name := range kept {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}
