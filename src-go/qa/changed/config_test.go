package changed

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want options
	}{
		{"none", nil, options{base: "HEAD"}},
		{"plan", []string{"--plan"}, options{base: "HEAD", planOnly: true}},
		{"base", []string{"--base", "origin/F44"}, options{base: "origin/F44"}},
		{"files", []string{"--files", "a", "b"}, options{base: "HEAD", explicit: true, files: []string{"a", "b"}}},
		{"flags after files are flags", []string{"--files", "a", "b", "--plan"}, options{base: "HEAD", explicit: true, files: []string{"a", "b"}, planOnly: true}},
		{"flags before and between", []string{"--plan", "--files", "a", "--base", "X", "b"}, options{base: "X", planOnly: true, explicit: true, files: []string{"a", "b"}}},
		{"double dash ends flags", []string{"--files", "--", "--plan", "-x"}, options{base: "HEAD", explicit: true, files: []string{"--plan", "-x"}}},
		{"help", []string{"--help"}, options{base: "HEAD", help: true}},
		{"short help", []string{"-h"}, options{base: "HEAD", help: true}},
		{"root and scripts", []string{"--root", "/r", "--scripts", "/r/s"}, options{base: "HEAD", root: "/r", scripts: "/r/s"}},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %v want %+v", c.name, got, err, c.want)
		}
	}
}

func TestParseArgsUsageErrors(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"base needs a ref":        {[]string{"--base"}, "--base needs a ref"},
		"base takes no flag":      {[]string{"--base", "--plan"}, "--base needs a ref"},
		"files needs files":       {[]string{"--files"}, "--files needs at least one file"},
		"files then only flags":   {[]string{"--files", "--plan"}, "--files needs at least one file"},
		"unknown flag":            {[]string{"--nope"}, "unknown argument '--nope'"},
		"unknown short flag":      {[]string{"--files", "a", "-x"}, "unknown argument '-x'"},
		"positional without flag": {[]string{"a.bash"}, "unknown argument 'a.bash'"},
		"root needs a value":      {[]string{"--root"}, "--root needs a directory"},
		"scripts needs a value":   {[]string{"--scripts"}, "--scripts needs a directory"},
	}
	for name, c := range cases {
		_, err := parseArgs(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
}

func TestTimeoutFromEnvironment(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"":     defaultTimeout,
		"0":    0,
		"90s":  90 * time.Second,
		"2h":   2 * time.Hour,
		"1m30": -1,
		"soon": -1,
		"-5m":  -1,
	} {
		got, err := parseTimeout(value)
		switch {
		case want == -1 && err == nil:
			t.Errorf("%q: want an error, got %v", value, got)
		case want != -1 && (err != nil || got != want):
			t.Errorf("%q: got %v, %v want %v", value, got, err, want)
		}
	}
}

func TestUsageNamesEveryFlag(t *testing.T) {
	for _, flag := range []string{"--base", "--plan", "--files", "--help", "QA_CHANGED_TIMEOUT", "QA_CHANGED_ROOT"} {
		if !strings.Contains(usageText, flag) {
			t.Errorf("usage lacks %s", flag)
		}
	}
}
