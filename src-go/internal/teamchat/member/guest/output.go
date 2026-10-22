package guest

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// What is one thing a guest op changed, the second field of its `CHANGED` line.
type What string

// Op is a guest operation's output contract: its verb, the fixed set of what it may report
// changed (TB2.4: the host refuses any other line), and whether it may exit 75, which only an
// op that takes a seat lock does (GU1).
type Op struct {
	Verb    string
	Changes []What
	Busy    bool
}

var registry struct {
	mu  sync.Mutex
	ops []Op
}

// register records op, so Ops and ChangesOf know it; each op's file registers its own, as
// `var OpX = register(Op{...})`. A verb registered twice panics, a mistake in this package.
func register(op Op) Op {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if slices.ContainsFunc(registry.ops, func(o Op) bool { return o.Verb == op.Verb }) {
		panic("guest: " + op.Verb + " registered twice")
	}
	op.Changes = slices.Clone(op.Changes)
	registry.ops = append(registry.ops, op)
	return op
}

// Ops is every guest op, each with a copy of its set.
func Ops() []Op {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	ops := make([]Op, len(registry.ops))
	for i, op := range registry.ops {
		op.Changes = slices.Clone(op.Changes)
		ops[i] = op
	}
	return ops
}

// ChangesOf is the set of what verb may report changed, for the host's parser of its stdout
// (TB2.4); false for a verb that is not a guest op.
func ChangesOf(verb string) ([]What, bool) {
	for _, op := range Ops() {
		if op.Verb == verb {
			return op.Changes, true
		}
	}
	return nil, false
}

// Output is a guest op's stdout: nothing but its `CHANGED<TAB><what>` lines.
type Output struct {
	w  io.Writer
	op Op
}

// NewOutput is op's stdout over w.
func NewOutput(w io.Writer, op Op) *Output { return &Output{w: w, op: op} }

// changedPrefix starts every line a guest op prints.
const changedPrefix = "CHANGED\t"

// Changed prints that what changed. A what outside the op's set panics: the host would refuse
// the line, so printing it is a mistake in the op's code.
func (o *Output) Changed(what What) error {
	if !slices.Contains(o.op.Changes, what) {
		panic(fmt.Sprintf("guest: %s may not report %q changed", o.op.Verb, what))
	}
	if _, err := io.WriteString(o.w, changedPrefix+string(what)+"\n"); err != nil {
		return fmt.Errorf("stdout: %w", err)
	}
	return nil
}

var errExit = errors.New("guest op failed with an exit outside its contract")

// Finish is op's exit for err, its error object written as the last line of stderr: 0, 64,
// 78, or 75 from an op that takes a seat lock (GU1). Any other failure is a bug in the op and
// is exit 1 `internal`.
func Finish(stderr io.Writer, op Op, err error) term.ExitCode {
	if e, ok := errors.AsType[*exits.Error](err); ok && !allowed(op, e.Exit) {
		err = fmt.Errorf("%w: %s, exit %d code %s", errExit, op.Verb, e.Exit, e.Code)
	}
	return exits.Write(stderr, err)
}

func allowed(op Op, exit term.ExitCode) bool {
	if exit == exits.ExitBusy {
		return op.Busy
	}
	return exit == exits.ExitUsage || exit == exits.ExitConfig
}
