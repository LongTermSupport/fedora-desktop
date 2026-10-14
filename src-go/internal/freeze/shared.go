package freeze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// GoneError is a group that vanished between the menu being drawn and the row chosen (a network
// removed, a bridge emptied, an identity value whose sessions are gone). It is recoverable from
// the menu, where SelectGone says it and the person is re-prompted, and fatal from the command
// line, where DieGone ends the run with the same words.
type GoneError struct{ Msg string }

func (g *GoneError) Error() string { return g.Msg }

// Gone formats a GoneError.
func Gone(format string, args ...any) error {
	return &GoneError{Msg: fmt.Sprintf(format, args...)}
}

// SelectGone is what an engine's Select does with the error its resolution returned: a
// GoneError is said on w as "<tool>: msg" and becomes ErrGroupGone; anything else is unchanged.
func SelectGone(w io.Writer, tool string, err error) error {
	gone, ok := errors.AsType[*GoneError](err)
	if !ok {
		return err
	}
	if sayErr := Say(w, "%s: %s\n", tool, gone.Msg); sayErr != nil {
		return sayErr
	}
	return ErrGroupGone
}

// DieGone is what an engine's SelectTarget does with it: a GoneError ends the run with the same
// words the menu path says; anything else is unchanged.
func DieGone(err error) error {
	if gone, ok := errors.AsType[*GoneError](err); ok {
		return Die("%s", gone.Msg)
	}
	return err
}

// Say writes a formatted note to w; a failed write is returned, not lost.
func Say(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return errors.Join(errors.New("write note"), err)
	}
	return nil
}

// FailureText is what a failed command said: stderr first, then any stdout, else the error.
func FailureText(res execx.Result, err error) string {
	var parts []string
	for _, s := range []string{string(res.Stderr), string(res.Stdout)} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return err.Error()
	}
	return strings.Join(parts, "\n")
}

// actPreparer is an optional Engine extension: BeforeAct runs once before each batch of acts, so
// an engine that needs something fresh for them (root, for one) gets it. An error ends the batch
// before any container is touched.
type actPreparer interface {
	BeforeAct(ctx context.Context) error
}
