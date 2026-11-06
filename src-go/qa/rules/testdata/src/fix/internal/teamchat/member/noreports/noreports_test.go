// Package noreports is a package that declares no reports join: the rule has nothing to judge, so
// the same call is not flagged.
package noreports

import (
	"testing"
	"time"

	"fix/internal/fakeclock"
)

func TestNamesAThirtySecondDue(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), 30*time.Second)
}
