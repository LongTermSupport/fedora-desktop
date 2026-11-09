package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The fixture starts fzf with and without the bindings for every Esc-joined key, and binds Esc
// in Bubble Tea with and without the esckey.Joined check.
func TestEscJoined(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), EscJoined,
		"fix/esc/internal/ui", "fix/esc/internal/ui/esckey", "fix/esc/internal/ui/badline", "fix/esc/internal/ui/goodline")
}
