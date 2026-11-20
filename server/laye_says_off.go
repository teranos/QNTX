//go:build !(cgo && rustetcd)

package server

import "github.com/teranos/QNTX/server/auth"

// layeSays in a build without ats-etcd: none.
func layeSays() auth.LayeSays { return nil }
