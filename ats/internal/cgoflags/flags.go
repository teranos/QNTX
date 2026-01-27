// Package cgoflags consolidates common CGO LDFLAGS for all Rust libraries.
//
// This package exists solely to specify shared system libraries once,
// eliminating duplicate library warnings when linking multiple Rust libraries.
//
// All CGO-based Rust wrappers (fuzzyax, sqlitecgo, vidstream) import this
// package to ensure system libraries are only specified once.
package cgoflags

/*
#cgo linux LDFLAGS: -lpthread -ldl -lm
#cgo darwin LDFLAGS: -lpthread -ldl -lm
#cgo windows LDFLAGS: -lws2_32 -luserenv
*/
import "C"

// This file intentionally left empty - CGO directives are the only content needed.
