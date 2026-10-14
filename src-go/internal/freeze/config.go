// Package freeze is the engine-neutral half of the freeze tools (podfreeze, lxcfreeze): the
// inventory and the pure decisions over it, the group menu and drill-down, the derived verb,
// the dry run and the act loop. An engine supplies only what is specific to it through the
// Engine interface and a Config.
package freeze

import (
	"errors"
	"fmt"
)

// Verb is what is done to a container. VerbDerived (the empty verb) means "let the state decide":
// anything running is frozen, a set with nothing running is thawed.
type Verb string

const (
	// VerbDerived is no explicit verb: the state decides.
	VerbDerived Verb = ""
	// VerbFreeze pauses the target containers.
	VerbFreeze Verb = "freeze"
	// VerbThaw unpauses them.
	VerbThaw Verb = "thaw"
)

const (
	// MaxTries is the number of bad replies a numbered prompt takes before it gives up.
	MaxTries = 3
	// AllRowKey is the drill-down's "act on all of it" row. A leading '*' cannot start a
	// container name, so it never collides with one.
	AllRowKey = "*all*"
)

// Config is what a tool declares about its engine.
type Config struct {
	Tool         string // "podfreeze": names the tool in every message
	StateRunning string // the engine's word for a running container
	StateFrozen  string // the engine's word for a frozen one; must differ from StateRunning
	HostOnlyNote string // why the tool cannot run in a container, appended to the refusal
	TargetHint   string // the targets the command line accepts, for the no-terminal error
	ListNote     string // optional: appended to the unknown-name error
	FreezeNote   string // optional: printed after the thaw hint on a freeze
}

// Validate reports the first required setting that is empty, or two identical state words. An
// engine that left one blank would otherwise compare states against the empty string.
func (c Config) Validate() error {
	required := []struct{ name, value string }{
		{"Tool", c.Tool},
		{"StateRunning", c.StateRunning},
		{"StateFrozen", c.StateFrozen},
		{"HostOnlyNote", c.HostOnlyNote},
		{"TargetHint", c.TargetHint},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("freeze: Config.%s is not set; every engine declares it", r.name)
		}
	}
	if c.StateRunning == c.StateFrozen {
		return fmt.Errorf("freeze: Config.StateRunning and Config.StateFrozen are both %q; they must differ", c.StateRunning)
	}
	return nil
}

// DieError is a fatal usage or engine error. Its message is printed as "<tool>: <message>" and
// the run exits 1.
type DieError struct{ Msg string }

func (e *DieError) Error() string { return e.Msg }

// Die formats a DieError.
func Die(format string, args ...any) error {
	return &DieError{Msg: fmt.Sprintf(format, args...)}
}

var (
	// ErrSomeFailed means at least one target failed. Each failure and the count were already
	// reported on stderr, so the exit status is all that is left to deliver.
	ErrSomeFailed = errors.New("some containers failed")
	// ErrGroupGone is the one recoverable failure an engine's Select may return: a network,
	// bridge or identity value that vanished between the menu being drawn and a row chosen. The
	// engine has already said so.
	ErrGroupGone = errors.New("the chosen group is gone")
	// ErrQuit is the person leaving the group menu.
	ErrQuit = errors.New("quit")
	// ErrBack is the person leaving the drill-down for the group menu.
	ErrBack = errors.New("back")
)
