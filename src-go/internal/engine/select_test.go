package engine

import (
	"errors"
	"os/exec"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// onPath is a lookPath that finds exactly the programs named.
func onPath(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(names, name) {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestSelectChoosesTheEngine(t *testing.T) {
	both := onPath("podman", "docker")
	cases := []struct {
		name string
		req  Request
		want Choice
	}{
		{"nothing set is podman", Request{}, Choice{Podman, FromDefault}},
		{"an empty variable is unset", Request{Env: ""}, Choice{Podman, FromDefault}},
		{"the variable names docker", Request{Env: "docker"}, Choice{Docker, FromEnv}},
		{"the variable names podman", Request{Env: "podman"}, Choice{Podman, FromEnv}},
		{"the flag names docker", Request{Flag: "docker", FlagSet: true}, Choice{Docker, FromFlag}},
		{"the flag wins over the variable", Request{Env: "docker", Flag: "podman", FlagSet: true}, Choice{Podman, FromFlag}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Select(c.req, both)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if got != c.want {
				t.Fatalf("Select = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSelectRefusesAnUnknownEngineAtSelection(t *testing.T) {
	cases := []struct {
		name     string
		req      Request
		wantFrom Source
		wantExit term.ExitCode
	}{
		// The bash launcher read this only at token validation, after the banners.
		{"the variable names nerdctl", Request{Env: "nerdctl"}, FromEnv, term.ExitFailure},
		// The bash launcher said "not found" for any name it could not run.
		{"the variable names buildah", Request{Env: "buildah"}, FromEnv, term.ExitFailure},
		{"the flag names lxc", Request{Flag: "lxc", FlagSet: true}, FromFlag, term.ExitUsage},
		{"the flag is empty", Request{Flag: "", FlagSet: true}, FromFlag, term.ExitUsage},
		{"upper case is not an engine's name", Request{Env: "Podman"}, FromEnv, term.ExitFailure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Select(c.req, onPath("podman", "docker", "nerdctl", "buildah", "lxc"))
			unknown, ok := errors.AsType[*UnknownError](err)
			if !ok {
				t.Fatalf("Select error = %v, want an *UnknownError", err)
			}
			if unknown.From != c.wantFrom {
				t.Fatalf("From = %v, want %v", unknown.From, c.wantFrom)
			}
			if got := Exit(err); got != c.wantExit {
				t.Fatalf("Exit = %d, want %d", got, c.wantExit)
			}
		})
	}
}

func TestSelectRefusesAnEngineThatIsNotInstalled(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want Choice
	}{
		{"the default", Request{}, Choice{Podman, FromDefault}},
		{"from the variable", Request{Env: "docker"}, Choice{Docker, FromEnv}},
		{"from the flag", Request{Flag: "docker", FlagSet: true}, Choice{Docker, FromFlag}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Select(c.req, onPath())
			missing, ok := errors.AsType[*NotFoundError](err)
			if !ok {
				t.Fatalf("Select error = %v, want a *NotFoundError", err)
			}
			if missing.Kind != c.want.Kind || missing.From != c.want.From {
				t.Fatalf("NotFoundError = %+v, want %+v", missing, c.want)
			}
			if Exit(err) != term.ExitFailure {
				t.Fatalf("Exit = %d, want %d", Exit(err), term.ExitFailure)
			}
		})
	}
}

func TestSelectReportsALookupThatFailedForAnotherReason(t *testing.T) {
	broken := errors.New("permission denied")
	_, err := Select(Request{}, func(string) (string, error) { return "", broken })
	if !errors.Is(err, broken) {
		t.Fatalf("Select error = %v, want it to wrap %v", err, broken)
	}
	if missing, ok := errors.AsType[*NotFoundError](err); ok && missing != nil {
		t.Fatal("a failed lookup is not an engine that is not installed")
	}
}

func TestFlagValueTakesTheNextArgument(t *testing.T) {
	args := []string{"--headless", "--engine", "docker", "--no-ssh"}
	got, err := FlagValue(args, 1)
	if err != nil || got != "docker" {
		t.Fatalf("FlagValue = %q, %v; want docker", got, err)
	}
}

func TestFlagValueRefusesADanglingFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		i    int
	}{
		// The bash launcher ignored this and launched with the default engine.
		{"the last argument", []string{"--no-ssh", "--engine"}, 1},
		// A separator is the end of ccy's own arguments, never an engine's name.
		{"followed by the separator", []string{"--engine", "--", "-p"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := FlagValue(c.args, c.i)
			if usage, ok := errors.AsType[*UsageError](err); !ok || usage == nil {
				t.Fatalf("FlagValue error = %v, want a *UsageError", err)
			}
			if Exit(err) != term.ExitUsage {
				t.Fatalf("Exit = %d, want %d", Exit(err), term.ExitUsage)
			}
		})
	}
}

func TestFlagValueRefusesAnIndexThatIsNotTheFlag(t *testing.T) {
	if _, err := FlagValue([]string{"--headless"}, 0); err == nil {
		t.Fatal("FlagValue accepted an argument that is not --engine")
	}
	if _, err := FlagValue([]string{"--engine"}, 3); err == nil {
		t.Fatal("FlagValue accepted an index past the arguments")
	}
}

func TestErrorTextNamesTheValueAndTheChoices(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&UnknownError{Value: "lxc", From: FromFlag}, "--engine names an unknown container engine: lxc (valid: docker, podman)"},
		{&UnknownError{Value: "nerdctl", From: FromEnv}, "CCY_CONTAINER_ENGINE names an unknown container engine: nerdctl (valid: docker, podman)"},
		{&NotFoundError{Kind: Docker, From: FromFlag}, "container engine docker (from --engine) is not installed: install it or choose another with --engine"},
		{&NotFoundError{Kind: Podman, From: FromDefault}, "container engine podman (the default) is not installed: install it or choose another with --engine"},
		{&NotFoundError{Kind: Docker, From: FromEnv}, "container engine docker (from CCY_CONTAINER_ENGINE) is not installed: install it or choose another with --engine"},
		{&UsageError{Msg: "--engine needs a value: docker or podman"}, "--engine needs a value: docker or podman"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
}

func TestExitOfAnyOtherErrorIsFailure(t *testing.T) {
	if Exit(errors.New("x")) != term.ExitFailure {
		t.Fatal("an unclassified error is a failure")
	}
	if Exit(nil) != term.ExitOK {
		t.Fatal("no error is success")
	}
}

func TestSourceAndKindStrings(t *testing.T) {
	if Podman.String() != "podman" || Docker.String() != "docker" {
		t.Fatal("a kind's string is its program's name")
	}
	if FromDefault.String() != "the default" || FromEnv.String() != "CCY_CONTAINER_ENGINE" || FromFlag.String() != "--engine" {
		t.Fatal("a source's string is where the choice came from")
	}
	if Source(9).String() != "an unknown source" {
		t.Fatal("an out-of-range source still prints")
	}
}
