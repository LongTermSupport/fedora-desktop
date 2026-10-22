package engine

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Kind is a container engine ccy can drive, named as its program is.
type Kind string

// The engines ccy drives. Podman is the default (CLAUDE/ContainerEngines.md); Docker is kept
// for compatibility (Plan 00191 decision D9).
const (
	Podman Kind = "podman"
	Docker Kind = "docker"
)

func (k Kind) String() string { return string(k) }

// valid is the list a refusal names, in the order the help text gives it.
const valid = "docker, podman"

// EnvName is the variable that chooses the engine when --engine does not.
const EnvName = "CCY_CONTAINER_ENGINE"

// FlagName is the launcher's option that chooses the engine.
const FlagName = "--engine"

// Source is where a choice of engine came from, for the messages that name it.
type Source int

// The places an engine can be chosen, weakest first.
const (
	FromDefault Source = iota
	FromEnv
	FromFlag
)

func (s Source) String() string {
	switch s {
	case FromDefault:
		return "the default"
	case FromEnv:
		return EnvName
	case FromFlag:
		return FlagName
	default:
		return "an unknown source"
	}
}

// Request is what the launcher was given: the variable's value (empty when unset) and the
// flag's value, if the flag was given.
type Request struct {
	Env     string
	Flag    string
	FlagSet bool
}

// Choice is the engine chosen and where the choice came from.
type Choice struct {
	Kind Kind
	From Source
}

// UsageError is a command line the launcher cannot read: exit 64.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// UnknownError is a name that is not an engine ccy drives.
type UnknownError struct {
	Value string
	From  Source
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("%s names an unknown container engine: %s (valid: %s)", e.From, e.Value, valid)
}

// NotFoundError is a chosen engine whose program is not on PATH.
type NotFoundError struct {
	Kind Kind
	From Source
}

func (e *NotFoundError) Error() string {
	from := "from " + e.From.String()
	if e.From == FromDefault {
		from = e.From.String()
	}
	return fmt.Sprintf("container engine %s (%s) is not installed: install it or choose another with %s", e.Kind, from, FlagName)
}

// Exit is the exit code an error from this package ends the launcher with: a command line
// that cannot be read, or an unknown name given on it, is a usage error; anything else a
// failure.
func Exit(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	if usage, ok := errors.AsType[*UsageError](err); ok && usage != nil {
		return term.ExitUsage
	}
	if unknown, ok := errors.AsType[*UnknownError](err); ok && unknown.From == FromFlag {
		return term.ExitUsage
	}
	return term.ExitFailure
}

// parse is the Kind a name is, if it is one; names are exact, as the programs' are.
func parse(name string) (Kind, bool) {
	switch Kind(name) {
	case Podman, Docker:
		return Kind(name), true
	default:
		return "", false
	}
}

// Select is the engine for this launch: the flag, else the variable, else podman. A name that
// is not an engine is refused here, wherever it came from, and so is an engine whose program
// lookPath cannot find.
func Select(r Request, lookPath func(string) (string, error)) (Choice, error) {
	choice := Choice{Kind: Podman, From: FromDefault}
	switch {
	case r.FlagSet:
		kind, ok := parse(r.Flag)
		if !ok {
			return Choice{}, &UnknownError{Value: r.Flag, From: FromFlag}
		}
		choice = Choice{Kind: kind, From: FromFlag}
	case r.Env != "":
		kind, ok := parse(r.Env)
		if !ok {
			return Choice{}, &UnknownError{Value: r.Env, From: FromEnv}
		}
		choice = Choice{Kind: kind, From: FromEnv}
	}
	if _, err := lookPath(string(choice.Kind)); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Choice{}, &NotFoundError{Kind: choice.Kind, From: choice.From}
		}
		return Choice{}, fmt.Errorf("look for %s on PATH: %w", choice.Kind, err)
	}
	return choice, nil
}

// FlagValue is the value of the --engine at args[i]: the next argument. A flag with nothing
// after it, or with the separator after it, is a usage error.
func FlagValue(args []string, i int) (string, error) {
	if i < 0 || i >= len(args) || args[i] != FlagName {
		return "", fmt.Errorf("engine.FlagValue: argument %d is not %s", i, FlagName)
	}
	if i+1 == len(args) || args[i+1] == "--" {
		return "", &UsageError{Msg: FlagName + " needs a value: " + string(Docker) + " or " + string(Podman)}
	}
	return args[i+1], nil
}
