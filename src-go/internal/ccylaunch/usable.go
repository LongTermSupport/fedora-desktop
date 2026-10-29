package ccylaunch

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F15, ccy-token-usable: whether a launch may start on a token, read with --token-usage's own
// reader (usageReport) from the API's rate-limit headers or the cache.

// ExitUsedUp is ccy-token-usable's exit code for a token with no usage left.
const ExitUsedUp term.ExitCode = 3

// usedUpPercent is the utilisation at which a window has no usage left.
const usedUpPercent = 100

const (
	usableUsage = "usage: ccy-token-usable [--mark] [--] TOKEN_FILE\n"
	markFlag    = "--mark"
	argsEnd     = "--"
	daysShown   = 6
	// bannerColour is bold, bright white, on red.
	bannerColour = "\x1b[1;97;41m"
	eraseToEnd   = "\x1b[K"
)

// UsageState is what is known of a token's usage.
type UsageState int

const (
	// UsageUnknown is usage that could not be read; Usability.Reason says why.
	UsageUnknown UsageState = iota
	// UsageLeft is usage left in both windows.
	UsageLeft
	// UsageUsedUp is a window with no usage left; Usability.UsedUp names each.
	UsageUsedUp
	// UsageFault is a fault of this machine (a setting, the pool, the cache, a missing curl)
	// that stopped the reading; Usability.Reason names it. Not a fact about the token.
	UsageFault
)

// UsedUpLimit is one window with no usage left.
type UsedUpLimit struct {
	Label  string    // "5-hour" or "7-day"
	Clears time.Time // when the window resets; zero when the API gave no time
}

// Usability is the answer to "may a session start on this token now, and if not, why and
// until when".
type Usability struct {
	Name   string // the token's name, never its value
	State  UsageState
	Reason string // UsageUnknown: why the usage could not be read
	UsedUp []UsedUpLimit
}

// Explain is the used-up windows in words, each with the clock time it clears and the time
// to go: "7-day limit used up; clears Sat 00:00, in 13h 40m".
func (u Usability) Explain(now time.Time) string {
	parts := make([]string, 0, len(u.UsedUp))
	for _, w := range u.UsedUp {
		clears := "the API gave no time it clears"
		if !w.Clears.IsZero() {
			clears = clearsText(w.Clears, now)
		}
		parts = append(parts, w.Label+" limit used up; "+clears)
	}
	return strings.Join(parts, ", and ")
}

// clearsText is "clears <clock time>, in <time to go>": today, tomorrow, a weekday within the
// week, else the date; the time to go rounded up to the minute.
func clearsText(at, now time.Time) string {
	at = at.In(now.Location())
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	days := int(day(at).Sub(day(now)).Hours()/24 + 0.5)
	var when string
	switch {
	case days == 0:
		when = "today"
	case days == 1:
		when = "tomorrow"
	case days <= daysShown:
		when = at.Format("Mon")
	default:
		when = at.Format("Mon 2 Jan")
	}
	minutes := int64((at.Sub(now) + time.Minute - 1) / time.Minute)
	minutes = max(minutes, 1)
	d, h, m := minutes/(secondsPerDay/secondsPerMin), minutes%(secondsPerDay/secondsPerMin)/secondsPerMin, minutes%secondsPerMin
	var togo string
	switch {
	case d > 0:
		togo = strconv.FormatInt(d, 10) + "d"
		if h > 0 {
			togo += " " + strconv.FormatInt(h, 10) + "h"
		}
	case h > 0:
		togo = strconv.FormatInt(h, 10) + "h"
		if m > 0 {
			togo += " " + strconv.FormatInt(m, 10) + "m"
		}
	default:
		togo = strconv.FormatInt(m, 10) + "m"
	}
	return "clears " + when + " " + at.Format("15:04") + ", in " + togo
}

// TokenUsability reads the usage of the token in file, which must be the file a name in the
// pool launches with. fetch asks the API unless the cache holds a fresh answer from that file,
// as --token-usage does; without it only the cache is read, however old, and a window whose
// reset has passed counts as clear.
func (f Front) TokenUsability(ctx context.Context, file string, fetch bool) Usability {
	u := Usability{Name: bashName(filepath.Base(file))}
	r := usageReport{f: f}
	// A bad setting is this machine's fault for every token. prepare's other reasons are about
	// the pool as a whole (any one odd file stops --token-usage's reading), so they are not
	// grounds to block a launch on this token: its usage is unknown.
	if _, why := f.usageSettings(); why != "" {
		u.State, u.Reason = UsageFault, why
		return u
	}
	if why := r.prepare(); why != "" {
		u.Reason = why
		return u
	}
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	if !r.s.off && !f.found(usageCurl) {
		u.State, u.Reason = UsageFault, r.err
		return u
	}
	if r.err != "" {
		u.Reason = r.err
		return u
	}
	at := -1
	for i, n := range r.names {
		if n.valid && filepath.Join(filepath.Clean(r.dir), n.file) == filepath.Clean(file) {
			at = i
		}
	}
	if at < 0 {
		u.Reason = "usage is read only for a token name's launch file in " + r.dir
		return u
	}
	var row usageRow
	if fetch {
		if err := r.prime(ctx, []int{at}); err != nil {
			u.State, u.Reason = UsageFault, err.Error()
			return u
		}
		row = r.row(r.names[at])
	} else {
		row = r.cachedRow(r.names[at])
	}
	return u.of(row, r.now)
}

// of is the answer the row's figures give: unknown without figures, else left or used up.
func (u Usability) of(row usageRow, now time.Time) Usability {
	if row.status != "ok" {
		u.Reason = row.reason
		if row.http == "429" && row.reason == usageStatusReason("429") {
			u.Reason = "the API refused the check (429) and sent no figures, so the session may be refused too"
		}
		return u
	}
	u.State = UsageLeft
	for _, w := range []struct{ label, pct, reset string }{{"5-hour", row.p5, row.r5}, {"7-day", row.p7, row.r7}} {
		if limit, used := usedUp(w.label, w.pct, w.reset, now); used {
			u.State = UsageUsedUp
			u.UsedUp = append(u.UsedUp, limit)
		}
	}
	return u
}

// usedUp is the window if it has no usage left: at or past the limit, and not reset since.
func usedUp(label, pct, reset string, now time.Time) (UsedUpLimit, bool) {
	p, err := strconv.ParseFloat(pct, 64)
	if pct == "" || err != nil || p < usedUpPercent {
		return UsedUpLimit{}, false
	}
	limit := UsedUpLimit{Label: label}
	if e, err := strconv.ParseInt(reset, 10, 64); reset != "" && err == nil {
		limit.Clears = time.Unix(e, 0)
		if !limit.Clears.After(now) {
			return UsedUpLimit{}, false
		}
	}
	return limit, true
}

// cachedRow is the cache's figures for the name, read with its launch file, however old.
func (r *usageReport) cachedRow(n usageName) usageRow {
	row := usageRow{status: "unavailable"}
	lines, why := r.entry(n.name)
	switch {
	case why != "":
		row.reason = why
	case line(lines, 0) != n.file:
		row.reason = "no figures were fetched for this token file"
	case line(lines, 2) == "":
		row.reason = usageStatusReason(line(lines, 1))
	default:
		r.figures(&row, line(lines, 2))
	}
	return row
}

// alarm is text as one line, or on a terminal (D10's rule) as a three-line banner of bold
// bright white on red, each line filled to its end (erase-to-end-of-line paints the
// background), so a refusal printed before the chooser cannot be missed.
func (f Front) alarm(w io.Writer, text string) string {
	if !f.colourOnFor(w) {
		return text + "\n"
	}
	fill := bannerColour + eraseToEnd + colourReset + "\n"
	return fill + bannerColour + " " + text + " " + eraseToEnd + colourReset + "\n" + fill
}

// TokenUsable is F15, the ccy-token-usable command.
func (f Front) TokenUsable(ctx context.Context) term.ExitCode {
	args := f.Args
	mark := len(args) > 0 && args[0] == markFlag
	if mark {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == argsEnd {
		args = args[1:]
	}
	var p term.Printer
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		p.Print(f.Stderr, usableUsage)
		if p.Err() != nil {
			return term.ExitFailure
		}
		return term.ExitUsage
	}
	u := f.TokenUsability(ctx, args[0], !mark)
	now := f.clock().Now()
	code := term.ExitOK
	switch {
	case mark:
		if u.State == UsageUsedUp {
			// Three spaces first: the chooser prints the mark straight after the row.
			p.Print(f.Stdout, "   ✗ no usage left: "+u.Explain(now)+"\n")
		}
	case u.State == UsageUsedUp:
		p.Print(f.Stderr, f.alarm(f.Stderr, fmt.Sprintf("✗ No usage left on token %s: %s. ccy will not start a session on it.", u.Name, u.Explain(now))))
		code = ExitUsedUp
	case u.State == UsageFault:
		p.Print(f.Stderr, "ccy-token-usable: "+u.Reason+"\n")
		code = term.ExitFailure
	case u.State == UsageUnknown:
		p.Print(f.Stderr, fmt.Sprintf("⚠ Could not check the usage left on token %s (%s), so ccy starts on it anyway.\n", u.Name, u.Reason))
	case u.State == UsageLeft:
	}
	if err := p.Err(); err != nil {
		return term.ExitFailure
	}
	return code
}
