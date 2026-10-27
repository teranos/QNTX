//go:build !quickdev

package reach

import _ "embed"

// The Tablet of Reach is the deployment's, compiled in: QNTX's own is for its
// tests and development, and a deployment builds with its own at this path.
//
//go:embed tablet-of-reach
var tablet string
