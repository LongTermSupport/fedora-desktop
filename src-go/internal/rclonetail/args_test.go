package rclonetail

import (
	"testing"
	"time"
)

// E1: the command line, read whole.
func TestParseArgs(t *testing.T) {
	cases := []struct {
		args []string
		want options
	}{
		{nil, options{interval: 5 * time.Second}},
		{[]string{"-o", "lib"}, options{interval: 5 * time.Second, once: true, filter: "lib"}},
		{[]string{"--interval", "2", "--once"}, options{interval: 2 * time.Second, once: true}},
		{[]string{"-i", "86400", "--", "-x"}, options{interval: 86400 * time.Second, filter: "-x"}},
		{[]string{"-"}, options{interval: 5 * time.Second, filter: "-"}},
		{[]string{"--bogus", "-h"}, options{help: true}},
		{[]string{"lib", "--help"}, options{help: true}},
		{[]string{"--", "-h"}, options{interval: 5 * time.Second, filter: "-h"}},
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
		{[]string{"--interval"}, "--interval needs a value"},
		{[]string{"-i", ""}, "--interval needs a value"},
		{[]string{"-i", "0"}, "--interval must be a whole number of seconds from 1 to 86400 (got '0')"},
		{[]string{"-i", "05"}, "--interval must be a whole number of seconds from 1 to 86400 (got '05')"},
		{[]string{"-i", "+5"}, "--interval must be a whole number of seconds from 1 to 86400 (got '+5')"},
		{[]string{"-i", "1.5"}, "--interval must be a whole number of seconds from 1 to 86400 (got '1.5')"},
		{[]string{"-i", "86401"}, "--interval must be a whole number of seconds from 1 to 86400 (got '86401')"},
		{
			[]string{"-i", "99999999999999999999"},
			"--interval must be a whole number of seconds from 1 to 86400 (got '99999999999999999999')",
		},
		{[]string{"a", "b"}, "only one mount name can be given (got 'a' and 'b')"},
		{[]string{"a", "--", "b"}, "only one mount name can be given (got 'a' and 'b')"},
	}
	for _, tc := range cases {
		_, err := parseArgs(tc.args)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q: %v, want %q", tc.args, err, tc.want)
		}
	}
}
