package main

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestDriverRegistersEveryRule(t *testing.T) {
	as := analyzers()
	if err := analysis.Validate(as); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"project_seam_exec": true, "project_seam_commands": true,
		"project_testclock": true, "project_homeabbrev": true, "project_package_tests": true,
	}
	for _, a := range as {
		delete(want, a.Name)
	}
	if len(want) != 0 {
		t.Fatalf("rules not registered: %v", want)
	}
}
