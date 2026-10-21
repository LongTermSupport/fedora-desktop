package rclonecache

import (
	"strings"
	"testing"
)

// E1: the grammar.
func TestParseArgs(t *testing.T) {
	cases := []struct {
		args []string
		want options
	}{
		{nil, options{}},
		{[]string{"gdrive"}, options{target: "gdrive", hasTarget: true}},
		{[]string{"--label", "before", "gdrive"}, options{label: "before", target: "gdrive", hasTarget: true}},
		{[]string{"-l", "after", "--no-dir"}, options{label: "after", noDir: true}},
		{[]string{"-l", "a", "-l", "b"}, options{label: "b"}},
		{[]string{"--", "-odd"}, options{target: "-odd", hasTarget: true}},
		{[]string{"-"}, options{target: "-", hasTarget: true}}, // B11
		{[]string{"-h"}, options{help: true}},
		{[]string{"--bogus", "--help"}, options{help: true}},
		{[]string{"a", "b", "--help"}, options{help: true}},
	}
	for _, tc := range cases {
		got, err := parseArgs(tc.args)
		if err != nil || got != tc.want {
			t.Errorf("%q: %+v %v, want %+v", tc.args, got, err, tc.want)
		}
	}
}

// E2: usage errors.
func TestParseArgsRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--bogus"}, "unknown option '--bogus'"},
		{[]string{"--label"}, "--label needs a value"},
		{[]string{"-l", ""}, "--label needs a value"},
		{[]string{"a", "b"}, "only one target can be given (got 'a' and 'b')"},
		{[]string{"a", "--", "b"}, "only one target can be given (got 'a' and 'b')"},
	}
	for _, tc := range cases {
		_, err := parseArgs(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.args, err, tc.want)
		}
	}
}
