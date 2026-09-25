// Package web is QNTX's stylesheets, for what QNTX draws outside a browser. A
// mail is drawn from them (ADR-041), so a mail looks the way these files say.
package web

import "embed"

//go:embed css/tokens.css css/canvas.css css/window.css css/element/title-bar.css css/element/states/window.css
var CSS embed.FS
