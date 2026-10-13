package declare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// repoRoot walks up from the test's directory to the checkout holding the real declare.py.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "agent_bus", "declare.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/agent_bus/declare.py above the test directory")
		}
		dir = parent
	}
}

func realClient(t *testing.T) *Client {
	t.Helper()
	file, err := filepath.Abs(filepath.Join("testdata", "host_vars.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Runner: execx.OS{}, Root: repoRoot(t), File: file}
}

func TestRealDeclared(t *testing.T) {
	teams, err := realClient(t).Declared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Team{
		{Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://192.0.2.10:6167"},
		{Name: "old", ServerName: "old.agent-bus.internal"},
	}
	if !reflect.DeepEqual(teams, want) {
		t.Fatalf("got %+v", teams)
	}
}

func TestRealLimited(t *testing.T) {
	limits, err := realClient(t).Limited(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Limit{{Team: "alpha", Prefixes: "CLAUDE/Plan/, docs/", Trusts: "any repository of example-org or example-org/proj"}}
	if !reflect.DeepEqual(limits, want) {
		t.Fatalf("got %+v", limits)
	}
}

func TestRealQueries(t *testing.T) {
	c := realClient(t)
	ctx := context.Background()
	if addr, err := c.Address(ctx); err != nil || addr != "192.0.2.10" {
		t.Fatalf("address %q %v", addr, err)
	}
	teams, err := c.TeamsFor(ctx, "example-org/proj")
	if err != nil || !slices.Equal(teams, []string{"alpha"}) {
		t.Fatalf("teams-for %v %v", teams, err)
	}
	remote, err := c.GithubRemote(ctx, "git@github.com:Example-Org/Proj.git")
	if err != nil || remote != "example-org/proj" {
		t.Fatalf("github-remote %q %v", remote, err)
	}
	remote, err = c.GithubRemote(ctx, "https://example.com/x/y")
	if err != nil || remote != "" {
		t.Fatalf("a non-GitHub remote gave %q %v", remote, err)
	}
	profiles, err := c.ElementTeams(ctx)
	if err != nil || len(profiles) != 0 {
		t.Fatalf("element-teams %+v %v", profiles, err)
	}
}

func TestRealRefusalsAreTyped(t *testing.T) {
	c := realClient(t)
	ctx := context.Background()
	err := c.CheckAddress(ctx, "not-an-ip")
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("check-address: %v", err)
	}
	if failure, ok := errors.AsType[*Error](err); !ok || failure.Code != 64 || failure.Message() == "" {
		t.Fatalf("check-address: %+v", failure)
	}
	if err := c.Check(ctx, Entry{Team: "alpha"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("a team that exists was accepted: %v", err)
	}
	if err := c.Check(ctx, Entry{Team: "beta", Humans: []string{"owner"}, Org: "example-org"}); err != nil {
		t.Fatalf("a valid entry was refused: %v", err)
	}
	missing := &Client{Runner: execx.OS{}, Root: c.Root, File: filepath.Join(t.TempDir(), "absent.yml")}
	if _, err := missing.Declared(ctx); !errors.Is(err, ErrRefused) {
		t.Fatalf("a missing host_vars: %v", err)
	}
}
