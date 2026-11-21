//go:build !(cgo && rustetcd)

package server

// namesAtLaye in a build without ats-etcd: names are the node's own lines.
func namesAtLaye(string) nameKeeper { return nil }
