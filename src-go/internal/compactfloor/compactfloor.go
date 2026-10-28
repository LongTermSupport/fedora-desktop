// Package compactfloor is the context size, in tokens, at or above which a session is typed
// /compact rather than left as it is: by `ccy-sessions set-going` after a reboot (Plan 00135
// Task 8.2), and by `ccy-teams wind-down` before a shutdown (Plan 00203 Task 3.20). Its one
// value is play-claude-yolo.yml's ccy_restore_compact_floor, written into the set-going unit's
// environment; set-going reads it from its own environment, and wind-down asks systemd for that
// unit's, so the two can never disagree.
package compactfloor

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// Env is the variable the floor is given in.
	Env = "CCY_RESTORE_COMPACT_FLOOR_TOKENS"
	// Unit is the user unit the play writes the floor into.
	Unit = "ccy-sessions-set-going.service"
	// where is what an error says the floor comes from.
	where = "ccy_restore_compact_floor_tokens in host_vars, or play-claude-yolo.yml's default, written by that play"
)

var wholeTokens = regexp.MustCompile(`^[1-9][0-9]*$`)

// Floor is a floor as it was written. It keeps the text, because a value that is not a whole
// number of tokens is reported as it was written, when it is needed.
type Floor struct {
	raw    string
	tokens int64
	valid  bool
}

// Parse reads a floor: a whole number of tokens, at least 1, written without a sign or leading
// zero. Anything else is a Floor whose Tokens are not valid.
func Parse(raw string) Floor {
	tokens, err := strconv.ParseInt(raw, 10, 64)
	if !wholeTokens.MatchString(raw) || err != nil {
		return Floor{raw: raw}
	}
	return Floor{raw: raw, tokens: tokens, valid: true}
}

// Tokens is the floor, and whether it is a whole number of tokens at all.
func (f Floor) Tokens() (int64, bool) { return f.tokens, f.valid }

// String is the floor as it was written.
func (f Floor) String() string { return f.raw }

// FromUnit is the floor in Unit's environment, which environment asks systemd for
// (systemd.Systemd.UserUnitEnvironment). A floor that is not there, or not a whole number of
// tokens, is an error naming where it comes from.
func FromUnit(ctx context.Context, environment func(context.Context, string) (string, error)) (Floor, error) {
	env, err := environment(ctx, Unit)
	if err != nil {
		return Floor{}, fmt.Errorf("the environment of %s could not be read (%s): %w", Unit, where, err)
	}
	for word := range strings.FieldsSeq(env) {
		raw, found := strings.CutPrefix(word, Env+"=")
		if !found {
			continue
		}
		if f := Parse(raw); f.valid {
			return f, nil
		}
		return Floor{}, fmt.Errorf("%s is '%s' in %s, not a whole number of tokens (%s)", Env, raw, Unit, where)
	}
	return Floor{}, fmt.Errorf("%s sets no %s (%s)", Unit, Env, where)
}
