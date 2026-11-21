//go:build !(cgo && rustetcd)

package server

// layesPasskeys in a build without ats-etcd: none. A node whose am.toml names
// an etcd is refused by the etcd window before it reads one.
func (s *QNTXServer) layesPasskeys() passkeysHeld { return nil }
