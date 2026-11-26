package loadgen

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
)

// The bounds of a run (LG4-LG6).
const (
	// MaxRate is the most units an hour of one pattern: ten a second.
	MaxRate = 36000
	// MaxLength is the longest run of the patterns.
	MaxLength = 168 * time.Hour
	// MaxSpeed is the most a replay is sped up.
	MaxSpeed = 3600
	// MaxReplayEvents is the most events a replay makes.
	MaxReplayEvents = 1_000_000
)

// Step is one thing a run does at AtMS milliseconds from its start: a pattern's unit, or a
// replayed kind.
type Step struct {
	AtMS          int64
	Pattern, Kind string
}

// Schedule gives a run's steps in order; false once there are no more.
type Schedule interface {
	Next() (Step, bool)
}

// rates is LG5's schedule: each pattern's next unit, by pattern order on a tie.
type rates struct {
	patterns []string
	every    []int64
	k        []int64
	untilMS  int64
}

// Rates is LG5's schedule over length: unit k of each pattern at k times an hour over its rate.
func Rates(perHour map[string]int, length time.Duration) (Schedule, error) {
	for p := range perHour {
		if !slices.Contains(Patterns, p) {
			return nil, fmt.Errorf("no pattern %q (the patterns are %s)", p, strings.Join(Patterns, ", "))
		}
	}
	if length < time.Second || length > MaxLength {
		return nil, fmt.Errorf("--for %v: give 1s to %v", length, MaxLength)
	}
	s := &rates{untilMS: length.Milliseconds()}
	for _, p := range Patterns {
		n, on := perHour[p]
		switch {
		case n < 0 || n > MaxRate:
			return nil, fmt.Errorf("--%s %d: give 0 (off) to %d units an hour", p, n, MaxRate)
		case !on || n == 0:
			continue
		}
		s.patterns = append(s.patterns, p)
		s.every = append(s.every, traffic.HourMS/int64(n))
		s.k = append(s.k, 0)
	}
	if len(s.patterns) == 0 {
		return nil, errors.New("no pattern is switched on: give at least one pattern a rate")
	}
	return s, nil
}

func (s *rates) Next() (Step, bool) {
	best := -1
	for i := range s.patterns {
		if at := s.k[i] * s.every[i]; at < s.untilMS && (best < 0 || at < s.k[best]*s.every[best]) {
			best = i
		}
	}
	if best < 0 {
		return Step{}, false
	}
	at := s.k[best] * s.every[best]
	s.k[best]++
	return Step{AtMS: at, Pattern: s.patterns[best]}, true
}

// listed is a schedule of steps already in order.
type listed struct{ steps []Step }

func (s *listed) Next() (Step, bool) {
	if len(s.steps) == 0 {
		return Step{}, false
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	return st, true
}

// shape is a row's room, typed key and relation, for the replay's table.
type shape struct{ room, typed, rel string }

// replayed is LG6's table: the kind each message shape is made as.
var replayed = map[shape]string{
	{proto.PurposeMain, proto.KeyPing, ""}:                       KindPing,
	{proto.PurposeMain, proto.KeyPing, traffic.RelThread}:        KindAnswer,
	{proto.PurposeMain, proto.KeyText, ""}:                       KindText,
	{proto.PurposeMain, proto.KeyText, traffic.RelReply}:         KindReply,
	{proto.PurposeMain, proto.KeyNotice, ""}:                     KindNotice,
	{proto.PurposeMain, "", ""}:                                  KindHumanMain,
	{proto.PurposeReports, proto.KeyReport, ""}:                  KindReport,
	{proto.PurposeReports, proto.KeyReceipt, ""}:                 KindReceiptEvent,
	{proto.PurposeQuestions, proto.KeyQuestion, ""}:              KindQuestion,
	{proto.PurposeQuestions, "", traffic.RelEdit}:                KindRetitle,
	{proto.PurposeQuestions, proto.KeyThread, traffic.RelThread}: KindThread,
	{proto.PurposeQuestions, "", traffic.RelThread}:              KindHumanThread,
}

// KindOf is the kind a row of the probe's counts is made as; false for a row the generator
// cannot make.
func KindOf(k traffic.Key) (string, bool) {
	if k.Type != proto.MessageType {
		return "", false
	}
	kind, ok := replayed[shape{k.Room, k.Typed, k.Rel}]
	return kind, ok
}

func describe(r traffic.Row) string {
	return fmt.Sprintf("%s %s %s %s %s (%d at %s)", r.Room, r.Type, dashed(r.Typed), dashed(r.Rel), dashed(r.Membership), r.Count, time.UnixMilli(r.HourMS).UTC().Format("2006-01-02T15:04Z"))
}

func dashed(s string) string { return cmp.Or(s, "-") }

// Replay is LG6's schedule: each row's events spread evenly over its hour, from the window's
// first hour, every offset divided by speed. A row it cannot make refuses the replay, or with
// skip is left out and named on warn.
func Replay(c traffic.Counts, speed int, skip bool, warn io.Writer) (Schedule, error) {
	if speed < 1 || speed > MaxSpeed {
		return nil, fmt.Errorf("--speed %d: give 1 to %d", speed, MaxSpeed)
	}
	first := c.Window.SinceMS - c.Window.SinceMS%traffic.HourMS
	var out []Step
	var unmade []string
	total := int64(0)
	for _, r := range c.Rows {
		kind, ok := KindOf(r.Key)
		if !ok {
			unmade = append(unmade, describe(r))
			continue
		}
		if total += r.Count; total > MaxReplayEvents {
			return nil, fmt.Errorf("the replay holds more than %d events: replay a shorter window", MaxReplayEvents)
		}
		for j := range r.Count {
			at := r.HourMS - first + j*traffic.HourMS/r.Count
			out = append(out, Step{AtMS: at / int64(speed), Kind: kind})
		}
	}
	if len(unmade) > 0 && !skip {
		return nil, fmt.Errorf("the replay holds %d rows the generator cannot make: %s; give --skip-unmade to leave them out", len(unmade), strings.Join(unmade, "; "))
	}
	for _, u := range unmade {
		if _, err := fmt.Fprintf(warn, "teamchat-loadgen: left out: %s\n", u); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("nothing to replay: the document holds no row the generator makes")
	}
	slices.SortStableFunc(out, func(a, b Step) int { return cmp.Compare(a.AtMS, b.AtMS) })
	return &listed{steps: out}, nil
}
