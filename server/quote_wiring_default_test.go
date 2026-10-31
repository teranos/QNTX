//go:build !qntxquote

package server

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// A build without qntxquote leaves the quote subsystem out and says so once,
// not once per built-in.
func TestABuildWithoutQuoteSaysSoOnce(t *testing.T) {
	core, seen := observer.New(zap.InfoLevel)
	s := &QNTXServer{logger: zap.New(core).Sugar()}

	s.setupQuote()

	if got := seen.Len(); got != 1 {
		t.Fatalf("a build without qntxquote logged %d lines, want 1: %v", got, seen.All())
	}
}
