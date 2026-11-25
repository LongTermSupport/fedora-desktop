// Package seatmodel is the model a teamchat seat starts on (Plan 00265 register R39): a default
// model per team, and a model per seat that overrides it, kept in one file of the owner's seat
// store (File, beside the pool, private and on no container mount), so every launch on this
// machine reads it without the network. `ccy-teams seats model` (Cmd) is the one way to set it.
//
// A launch (teamchatlaunch) asks For over its seats, in the launch's order: the first seat with a
// model decides (its own, else its team's default); a later seat asking for another is named in
// the launch's note. The model reaches claude as ANTHROPIC_MODEL (EnvVar), which claude ranks
// below a --model on its command line, so a model the person names there wins without any
// argument being read here; an ANTHROPIC_MODEL the person set is kept. A model name is passed
// through as given: Check only holds it to one non-empty token.
package seatmodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// EnvVar is the variable claude reads its model from when no --model is given.
const EnvVar = "ANTHROPIC_MODEL"

const fileName = "models.json"

// The refusals: a model name that is not one token, and a models file that cannot be trusted.
var (
	ErrModel = errors.New("not a model name")
	ErrFile  = errors.New("the seat models file is not usable")
)

// Team is one team's models: its default, and each seat's own by its handle.
type Team struct {
	Default string            `json:"default,omitempty"`
	Seats   map[string]string `json:"seats,omitempty"`
}

// Settings is the whole file, by team id.
type Settings struct {
	Teams map[string]Team `json:"teams"`
}

// File is where the models are kept: the seat store's root.
func File(store paths.Store) string { return store.Root() + "/" + fileName }

// Check holds a model name to one token: not empty, no space or control character, and not an
// option (a leading -).
func Check(model string) error {
	if model == "" || strings.HasPrefix(model, "-") || strings.ContainsFunc(model, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return fmt.Errorf("%w: %q (one word, as claude --model takes it: opus, sonnet or a full model id)", ErrModel, model)
	}
	return nil
}

// Read is the store's models; no file is none.
func Read(store paths.Store) (Settings, error) {
	path := File(store)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read the seat models %s: %w", path, err)
	}
	s, err := decode(data)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %s: %w; remove the file, then set the models again with ccy-teams seats model", ErrFile, path, err)
	}
	return s, nil
}

func decode(data []byte) (Settings, error) {
	var s Settings
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("decode: %w", err)
	}
	if dec.More() {
		return Settings{}, errors.New("more than one JSON value")
	}
	for id, t := range s.Teams {
		if _, err := paths.ParseTeamID(id); err != nil {
			return Settings{}, fmt.Errorf("team %q: %w", id, err)
		}
		if t.Default != "" {
			if err := Check(t.Default); err != nil {
				return Settings{}, fmt.Errorf("team %s: %w", id, err)
			}
		}
		for name, model := range t.Seats {
			if _, err := paths.ParseSeatName(name); err != nil {
				return Settings{}, fmt.Errorf("team %s: %w", id, err)
			}
			if err := Check(model); err != nil {
				return Settings{}, fmt.Errorf("team %s seat %s: %w", id, name, err)
			}
		}
	}
	return s, nil
}

// Write stores s in the store, 0600 and atomically.
func Write(store paths.Store, s Settings) error {
	if s.Teams == nil {
		s.Teams = map[string]Team{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the seat models: %w", err)
	}
	if err := os.MkdirAll(store.Root(), 0o700); err != nil {
		return fmt.Errorf("make the seat store: %w", err)
	}
	return fsx.WriteAtomic(File(store), "seat models", append(data, '\n'), 0o600)
}

// SetDefault sets team's default model; "" removes it.
func (s *Settings) SetDefault(team paths.TeamID, model string) {
	t := s.Teams[team.String()]
	t.Default = model
	s.put(team, t)
}

// SetSeat sets seat's own model in team; "" removes it.
func (s *Settings) SetSeat(team paths.TeamID, seat paths.SeatName, model string) {
	t := s.Teams[team.String()]
	seats := maps.Clone(t.Seats)
	if seats == nil {
		seats = map[string]string{}
	}
	if model == "" {
		delete(seats, seat.String())
	} else {
		seats[seat.String()] = model
	}
	t.Seats = seats
	s.put(team, t)
}

// put keeps t, or drops the team when it has no model left.
func (s *Settings) put(team paths.TeamID, t Team) {
	if s.Teams == nil {
		s.Teams = map[string]Team{}
	}
	if t.Default == "" && len(t.Seats) == 0 {
		delete(s.Teams, team.String())
		return
	}
	s.Teams[team.String()] = t
}

// Seat is one seat of a launch.
type Seat struct {
	Team paths.TeamID
	Name paths.SeatName
}

// Pick is the model a launch starts on: which seat gave it, whether it was the seat's own (else
// its team's default), and every later seat asking for another, as "<model> (<seat> of team
// <team>)". No Model: no seat has one, and claude takes the account's default.
type Pick struct {
	Model    string
	Seat     Seat
	FromSeat bool
	Others   []string
}

// of is seat's model and whether it is the seat's own.
func (s Settings) of(seat Seat) (string, bool) {
	t := s.Teams[seat.Team.String()]
	if m := t.Seats[seat.Name.String()]; m != "" {
		return m, true
	}
	return t.Default, false
}

// For is the model of seats, the first with one deciding.
func (s Settings) For(seats []Seat) Pick {
	var p Pick
	for _, seat := range seats {
		model, own := s.of(seat)
		switch {
		case model == "":
		case p.Model == "":
			p = Pick{Model: model, Seat: seat, FromSeat: own}
		case model != p.Model:
			p.Others = append(p.Others, model+" ("+seat.Name.String()+" of team "+seat.Team.String()+")")
		}
	}
	return p
}

// Note is the launch's line about p (p has a Model).
func (p Pick) Note() string {
	from := "team " + p.Seat.Team.String() + "'s model"
	if p.FromSeat {
		from = "the model of seat " + p.Seat.Name.String() + " of team " + p.Seat.Team.String()
	}
	note := "Model: " + p.Model + ", " + from + " (a --model given to claude wins; ccy-teams seats model changes it)."
	if len(p.Others) > 0 {
		note += "\n  Not used, as the launch's first seat with a model decides: " + strings.Join(p.Others, ", ") + "."
	}
	return note
}

// Verb is `ccy-teams seats model`.
const Verb = "model"

const (
	usageLine = "ccy-teams seats model [<team> <model>|--clear] [[--host] <seat>@<team> <model>|--clear]"
	prefix    = "ccy-teams seats model: "
	flagClear = "--clear"
	flagHost  = "--host"
	kindHost  = "host"
	kindPod   = "podman"
)

// errUsage is an argument the verb cannot use (64).
var errUsage = errors.New("usage")

// Cmd is `ccy-teams seats model`: with no argument, every model set, one a line (`<team> team
// <model>`, then `<team> <seat> <model>`); with a team or a seat and a model, that model set;
// with --clear in place of the model, removed. A seat is named as a launch names it,
// <seat>@<team>, in the working directory's checkout, a ccy seat unless --host (a cc seat).
type Cmd struct {
	Stdout, Stderr io.Writer
	// Store is the owner's seat store.
	Store func() (paths.Store, error)
	// Resolve is the working directory checkout's pool seat of team of kind whose seat part is
	// seat.
	Resolve func(team paths.TeamID, seat, kind string) (paths.SeatName, error)
}

// request is one run's arguments.
type request struct {
	list       bool
	team       paths.TeamID
	seat, kind string
	model      string
}

// Run is the verb over its arguments (after `seats model`).
func (c *Cmd) Run(args []string) term.ExitCode {
	err := c.run(args)
	switch {
	case err == nil:
		return term.ExitOK
	case errors.Is(err, errUsage):
		return c.say(term.ExitUsage, prefix+err.Error()+". Usage: "+usageLine+". See ccy-teams --help.")
	}
	return c.say(term.ExitFailure, prefix+err.Error())
}

func (c *Cmd) say(code term.ExitCode, msg string) term.ExitCode {
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}

func parse(args []string) (request, error) {
	var words []string
	removing, host := false, false
	for _, a := range args {
		switch {
		case a == flagClear:
			removing = true
		case a == flagHost:
			host = true
		case strings.HasPrefix(a, "-"):
			return request{}, fmt.Errorf("%w: %q is not an option of this verb", errUsage, a)
		default:
			words = append(words, a)
		}
	}
	if len(words) == 0 && !removing && !host {
		return request{list: true}, nil
	}
	want := 2
	if removing {
		want = 1
	}
	if len(words) != want {
		return request{}, fmt.Errorf("%w: give a team or a seat, then a model or --clear", errUsage)
	}
	r := request{kind: kindPod}
	if host {
		r.kind = kindHost
	}
	teamPart := words[0]
	seat, t, isSeat := strings.Cut(words[0], "@")
	if isSeat {
		r.seat, teamPart = seat, t
	}
	if isSeat && r.seat == "" {
		return request{}, fmt.Errorf("%w: %q names no seat before the @", errUsage, words[0])
	}
	if host && !isSeat {
		return request{}, fmt.Errorf("%w: --host names a cc seat, <seat>@<team>; a team's model is every kind's", errUsage)
	}
	team, err := paths.ParseTeamID(teamPart)
	if err != nil {
		return request{}, fmt.Errorf("%w: %w", errUsage, err)
	}
	r.team = team
	if !removing {
		if err := Check(words[1]); err != nil {
			return request{}, fmt.Errorf("%w: %w", errUsage, err)
		}
		r.model = words[1]
	}
	return r, nil
}

func (c *Cmd) run(args []string) error {
	r, err := parse(args)
	if err != nil {
		return err
	}
	store, err := c.Store()
	if err != nil {
		return err
	}
	s, err := Read(store)
	if err != nil {
		return err
	}
	if r.list {
		return c.list(s)
	}
	what := "team " + r.team.String() + "'s seats"
	if r.seat == "" {
		s.SetDefault(r.team, r.model)
	} else {
		name, err := c.Resolve(r.team, r.seat, r.kind)
		if err != nil {
			return err
		}
		s.SetSeat(r.team, name, r.model)
		what = "seat " + name.String() + " of team " + r.team.String()
	}
	if err := Write(store, s); err != nil {
		return err
	}
	msg := prefix + what + " start on " + r.model + " from their next launch."
	if r.model == "" {
		msg = prefix + what + ": model removed from the next launch on."
	}
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil {
		return fmt.Errorf("could not say what was set: %w", err)
	}
	return nil
}

// list prints every model set, teams in order, each team's default before its seats.
func (c *Cmd) list(s Settings) error {
	var lines []string
	for _, id := range slices.Sorted(maps.Keys(s.Teams)) {
		t := s.Teams[id]
		if t.Default != "" {
			lines = append(lines, id+" team "+t.Default)
		}
		for _, name := range slices.Sorted(maps.Keys(t.Seats)) {
			lines = append(lines, id+" "+name+" "+t.Seats[name])
		}
	}
	if len(lines) == 0 {
		if _, err := io.WriteString(c.Stderr, prefix+"no model is set: every seat starts on the account's default.\n"); err != nil {
			return fmt.Errorf("could not say so: %w", err)
		}
		return nil
	}
	if _, err := io.WriteString(c.Stdout, strings.Join(lines, "\n")+"\n"); err != nil {
		return fmt.Errorf("could not print the models: %w", err)
	}
	return nil
}
