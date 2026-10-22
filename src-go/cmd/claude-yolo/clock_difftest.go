//go:build difftest

package main

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch/difftest/pinclock"
)

// clock is the differential harness's pinned clock, in a build for the harness only
// (go build -tags difftest): such a build refuses to run outside a scenario's sandbox.
func clock() (ccylaunch.Clock, error) { return pinclock.FromEnv() }
