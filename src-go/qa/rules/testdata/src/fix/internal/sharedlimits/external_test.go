package sharedlimits_test

import (
	"testing"

	"fix/internal/fakeclock"
	"fix/internal/sharedlimits"
)

// An external test is judged against the package it tests.
func TestFromOutside(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), sharedlimits.RequestLimit) // want `project_shared_due`
	_ = clk.AwaitDue(t.Context(), sharedlimits.IdleLimit)
}
