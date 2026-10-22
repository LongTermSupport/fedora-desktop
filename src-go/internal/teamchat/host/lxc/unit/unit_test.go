package unit

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func session(t *testing.T) Session {
	t.Helper()
	u, err := paths.ParseGuestUser("agent1")
	if err != nil {
		t.Fatal(err)
	}
	return Session{User: u, Group: "agents", Home: "/srv/agent1", Checkout: "/srv/agent1/src/repo.1"}
}

// The golden file is C 3.9.4's unit (DESIGN-v2-C-host.md, the session unit) with its values in.
func TestRenderIsTheDesignsUnit(t *testing.T) {
	want, err := os.ReadFile("testdata/session.service")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(session(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRefusesAValueOutsideItsGrammar(t *testing.T) {
	cases := map[string]struct {
		edit  func(*Session)
		field string
	}{
		"no user":                 {func(s *Session) { s.User = paths.GuestUser{} }, "user"},
		"no group":                {func(s *Session) { s.Group = "" }, "group"},
		"a group with a capital":  {func(s *Session) { s.Group = "Agents" }, "group"},
		"a group with a space":    {func(s *Session) { s.Group = "a b" }, "group"},
		"a home with a space":     {func(s *Session) { s.Home = "/home/a b" }, "home"},
		"a home with a percent":   {func(s *Session) { s.Home = "/home/%u" }, "home"},
		"a home with a dollar":    {func(s *Session) { s.Home = "/home/$USER" }, "home"},
		"a home with a newline":   {func(s *Session) { s.Home = "/home/a\nExecStartPre=/bin/sh" }, "home"},
		"a relative home":         {func(s *Session) { s.Home = "home/agent1" }, "home"},
		"a home with ..":          {func(s *Session) { s.Home = "/home/../root" }, "home"},
		"a home ending in /":      {func(s *Session) { s.Home = "/srv/agent1/" }, "home"},
		"the root as home":        {func(s *Session) { s.Home = "/" }, "home"},
		"no home":                 {func(s *Session) { s.Home = "" }, "home"},
		"a checkout with ..":      {func(s *Session) { s.Checkout = "/srv/agent1/.." }, "checkout"},
		"a checkout with //":      {func(s *Session) { s.Checkout = "/srv//agent1" }, "checkout"},
		"a checkout with a quote": {func(s *Session) { s.Checkout = `/home/"x"` }, "checkout"},
		"the root as checkout":    {func(s *Session) { s.Checkout = "/" }, "checkout"},
		"a relative checkout":     {func(s *Session) { s.Checkout = "./repo" }, "checkout"},
	}
	for name, c := range cases {
		s := session(t)
		c.edit(&s)
		text, err := Render(s)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.field) || text != nil {
			t.Errorf("%s: %q %v", name, text, err)
		}
	}
}

// CL5 for SU1: the rendered ExecStart, after tmux's own arguments, is a launch run's own parser
// takes as an LXC Claude Code session (B 2.4.1).
func TestExecStartParsesAsAnLXCLaunch(t *testing.T) {
	text, err := Render(session(t))
	if err != nil {
		t.Fatal(err)
	}
	words, err := ExecStart(text)
	if err != nil {
		t.Fatal(err)
	}
	tmux := []string{"/usr/bin/tmux", "-L", paths.TmuxSocket, "new-session", "-d", "-s", paths.TmuxSession}
	if !slices.Equal(words[:len(tmux)], tmux) {
		t.Fatalf("tmux's arguments: %q", words)
	}
	rest := words[len(tmux):]
	if len(rest) < 2 || rest[0] != paths.MemberBinaryPath || rest[1] != "run" {
		t.Fatalf("after tmux: %q", rest)
	}
	l, err := cli.ParseLaunch(rest[2:])
	if err != nil {
		t.Fatal(err)
	}
	checkLXCLaunch(t, l)
}

func checkLXCLaunch(t *testing.T, l cli.Launch) {
	t.Helper()
	if l.Mode != cli.LaunchClaude || l.Kind != cli.KindLXC || l.Desired != "/srv/agent1/.config/teamchat/membership" ||
		!l.ResumeRecorded || !slices.Equal(l.Cmd, []string{"claude"}) || l.Claude != 0 || l.State != "" || l.Kit != "" || l.Seats != nil {
		t.Errorf("%+v", l)
	}
}

func TestExecStartRefusesAnythingButOneSingleSpacedLine(t *testing.T) {
	for name, text := range map[string]string{
		"none":         "[Service]\nType=forking\n",
		"two":          "ExecStart=/bin/a\nExecStart=/bin/b\n",
		"empty":        "ExecStart=\n",
		"double space": "ExecStart=/bin/a  b\n",
		"a tab":        "ExecStart=/bin/a\tb\n",
		"a lead space": "ExecStart= /bin/a\n",
	} {
		words, err := ExecStart([]byte(text))
		if !errors.Is(err, ErrInvalid) || words != nil {
			t.Errorf("%s: %q %v", name, words, err)
		}
	}
	words, err := ExecStart([]byte("[Service]\nExecStart=/bin/a b c\nRestart=always"))
	if err != nil || !slices.Equal(words, []string{"/bin/a", "b", "c"}) {
		t.Errorf("%q %v", words, err)
	}
}
