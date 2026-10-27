package ccc

import (
	"slices"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want options
	}{
		{nil, options{Agent: Claude}},
		{[]string{"--claude"}, options{Agent: Claude}},
		{[]string{"--continue", "--token", "work"}, options{Agent: Claude, Continue: true, Token: "work"}},
		{[]string{"-h"}, options{Agent: Claude, Help: true}},
	} {
		got, err := parseArgs(c.args)
		if err != nil || got.Agent.Name != c.want.Agent.Name || got.Continue != c.want.Continue || got.Token != c.want.Token || got.Help != c.want.Help {
			t.Fatalf("%v: got %+v, %v", c.args, got, err)
		}
	}
}

func TestEveryAgentHasAFlagAndACommand(t *testing.T) {
	for _, a := range agents {
		if a.Flag != "--"+a.Name || len(a.Command(false)) == 0 || a.GuideFile == "" || a.LoginEnv == "" {
			t.Fatalf("agent %+v", a)
		}
	}
	if !slices.Equal(Claude.Command(true), []string{"claude", "--dangerously-skip-permissions", "--continue"}) {
		t.Fatalf("claude %v", Claude.Command(true))
	}
}
