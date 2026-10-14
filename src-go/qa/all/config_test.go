package all

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseArgsAcceptsHelpAndTheInternalFlags(t *testing.T) {
	opts, err := parseArgs([]string{"--root", "/r", "--scripts", "/r/scripts"})
	if err != nil || opts.root != "/r" || opts.scripts != "/r/scripts" || opts.help {
		t.Fatalf("got %+v, %v", opts, err)
	}
	for _, flag := range []string{"-h", "--help"} {
		if opts, err := parseArgs([]string{flag}); err != nil || !opts.help {
			t.Fatalf("%s: %+v, %v", flag, opts, err)
		}
	}
	if opts, err := parseArgs(nil); err != nil || opts.help || opts.root != "" {
		t.Fatalf("no arguments: %+v, %v", opts, err)
	}
}

// Bug B1: the bash ignored every argument and ran the whole suite.
func TestParseArgsRefusesWhatItDoesNotKnow(t *testing.T) {
	cases := []struct {
		args     []string
		mentions string
		usage    bool
	}{
		{[]string{"--typo"}, "unknown argument '--typo'", true},
		{[]string{"extra"}, "unknown argument 'extra'", true},
		{[]string{"--root"}, "--root needs a directory", false},
		{[]string{"--root", "--help"}, "--root needs a directory", false},
		{[]string{"--scripts"}, "--scripts needs a directory", false},
	}
	for _, c := range cases {
		_, err := parseArgs(c.args)
		if err == nil || !strings.Contains(err.Error(), c.mentions) {
			t.Fatalf("%v: got %v", c.args, err)
		}
		usage, ok := errors.AsType[*usageError](err)
		if !ok || usage.listUsage != c.usage {
			t.Fatalf("%v: usage listing %+v", c.args, usage)
		}
	}
}

func TestParseTimeout(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", defaultTimeout, false},
		{"0", 0, false},
		{"90s", 90 * time.Second, false},
		{"2h", 2 * time.Hour, false},
		{"soon", 0, true},
		{"-5m", 0, true},
	}
	for _, c := range cases {
		got, err := parseTimeout(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%q: got %v, %v", c.in, got, err)
		}
	}
}
