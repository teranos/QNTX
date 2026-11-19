//go:build !(cgo && rustetcd)

package server

// 「[Opus 5.5] collectedKeysOfNode in a build without ats-etcd: none. A node whose
// 「[Opus 5.5] am.toml names an etcd is refused by the etcd window before it collects.」」
func (s *QNTXServer) collectedKeysOfNode() collectedKeeper { return nil }
