//go:build !difftest

package main

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"

// clock is the machine's: the installed program reads no other (clock_difftest.go is the
// harness's build).
func clock() (ccylaunch.Clock, error) { return ccylaunch.SystemClock{}, nil }
