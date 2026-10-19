// Package settings is a team's settings screen, the team row's "Edit settings" in the picker:
// every field of the team's agent_bus_teams entry with its value. An editable field is changed
// in place and judged, with the other pending changes, by declare.py set --check; Save and apply
// writes them in one declare.py set and applies them through ops.ApplyTeams, the path of
// ccy-teams apply. The coordinator is changed at once, through seats.ChangeRole. A fixed field
// says why it is fixed.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// maxTries is how often a refused value is asked again before the field is left as it was.
const maxTries = 3

// The tags of the rows that are not a field.
const (
	tagCoordinator = "coordinator"
	tagSave        = "save"
	tagDiscard     = "discard"
	tagNoOne       = "-"
)

// Settings is the screen. Every dependency is a field so a test stands in for each.
type Settings struct {
	Kit     *flow.Kit
	Declare *declare.Client
	Bus     *agentbus.Client
	// Apply is ops.ApplyTeams: the apply play for those teams, as ccy-teams apply runs it.
	Apply func(context.Context, []string) error
	// Locked is ops.Locked: fn run in one hold of the play lock, waited for first. Save and
	// apply writes and applies in it, so a wait that does not end in the lock saves nothing.
	Locked func(ctx context.Context, purpose string, fn func(context.Context) error) error
	// ChangeRole is seats.ChangeRole, the one code path that changes a member's role: it asks the
	// plain offer or the takeover (package coordinator), and demotes and promotes with roll-back.
	ChangeRole func(ctx context.Context, team, handle string, want bool, look coordinator.Lookup) (bool, error)
}

// New is the screen over a host, writing and applying in locked's hold of the play lock,
// applying through apply and changing roles through changeRole.
func New(h *host.Host, kit *flow.Kit, apply func(context.Context, []string) error,
	locked func(ctx context.Context, purpose string, fn func(context.Context) error) error,
	changeRole func(ctx context.Context, team, handle string, want bool, look coordinator.Lookup) (bool, error),
) *Settings {
	return &Settings{Kit: kit, Declare: h.Declare, Bus: h.Bus, Apply: apply, Locked: locked, ChangeRole: changeRole}
}

// screen is one visit to a team's settings: the entry as declared, the changes not yet saved
// (JSON by key), the team's members and what is known of their sessions, and the line saying
// what the last choice did.
type screen struct {
	s        *Settings
	team     string
	declared []declare.Setting
	pending  map[string]string
	roster   agentbus.Roster
	rosterOK bool
	look     coordinator.Lookup
	notice   string
}

// Edit is the team's settings screen, until Back, or Save and apply. look is what the caller
// knows of each member's session (the picker's seat rows), so a takeover can say whether the
// current coordinator runs.
func (s *Settings) Edit(ctx context.Context, team string, look coordinator.Lookup) (flow.Result, error) {
	declared, err := s.Declare.Settings(ctx, team)
	if err != nil {
		s.Kit.ReportErr(err)
		return s.Kit.Note(ctx, "The settings of "+team+" could not be read; nothing was changed.")
	}
	sc := &screen{s: s, team: team, declared: declared, pending: map[string]string{}, look: look}
	sc.readRoster(ctx)
	for {
		if err := s.Kit.Interrupted(ctx); err != nil {
			return flow.Done, err
		}
		done, res, err := sc.once(ctx)
		if err != nil || done {
			return res, err
		}
	}
}

func (sc *screen) readRoster(ctx context.Context) {
	roster, err := sc.s.Bus.List(ctx, sc.team)
	if err != nil {
		sc.s.Kit.ReportRootErr(err)
	}
	sc.roster, sc.rosterOK = roster, err == nil
}

// title is the screen's title, with the step below it when there is one.
func (sc *screen) title(step string) string {
	t := "Team " + sc.team + " › Settings"
	if step != "" {
		t += " › " + step
	}
	return t
}

func (sc *screen) header(about string) string {
	return ui.Header("Enter choose   Esc or q back",
		"Settings of team "+sc.team+": changes wait here until Save and apply.", sc.notice, about)
}

// once draws the screen and does what was chosen; done ends the visit.
func (sc *screen) once(ctx context.Context) (bool, flow.Result, error) {
	rows, tags, headers := sc.rows()
	picked, err := sc.s.Kit.Choose(ctx, ui.PickSpec{
		Title: sc.title(""), Header: sc.header(""), RowHeaders: headers, Rows: rows, Tags: tags, ExitLabel: "Back",
	})
	sc.notice = ""
	if errors.Is(err, ui.ErrLeave) {
		if stop := sc.s.Kit.Interrupted(ctx); stop != nil {
			return true, flow.Done, stop
		}
		leave, backErr := sc.back(ctx)
		return leave, flow.Done, backErr
	}
	if err != nil {
		return true, flow.Done, err
	}
	switch picked.Tag {
	case tagSave:
		return sc.save(ctx)
	case tagDiscard:
		sc.notice = "Nothing to discard."
		if n := len(sc.pending); n > 0 {
			sc.notice = "Discarded " + changes(n) + "."
		}
		sc.pending = map[string]string{}
		return false, flow.Done, nil
	case tagCoordinator:
		return false, flow.Done, sc.coordinator(ctx)
	}
	f := fieldOf(picked.Tag)
	if f.key == "" {
		sc.s.Kit.Term.PrintError("the settings screen returned a choice that is not in it: " + picked.Tag)
		return true, flow.Done, flow.Leave(term.ExitFailure)
	}
	return false, flow.Done, sc.editField(ctx, f)
}

// rows are the editable fields, the coordinator, the fixed fields, then Save and Discard; each
// with a header saying what it is and what applying it does.
func (sc *screen) rows() ([]string, []string, []string) {
	var rows, tags, headers []string
	add := func(row, tag, about string) {
		rows, tags, headers = append(rows, row), append(tags, tag), append(headers, sc.header(about))
	}
	for _, f := range fields {
		if f.fixed != "" {
			continue
		}
		row := f.label + ": " + sc.value(f.key)
		if _, changed := sc.pending[f.key]; changed {
			row += "   (changed)"
		}
		add(row, f.key, f.about+" "+f.applies)
	}
	add("Coordinator: "+sc.coordinatorName()+"   (changed at once)", tagCoordinator,
		"Changed at once, not by Save and apply: the coordinator is copied on what workers tell the "+
			"humans, and takes the humans' requests to dispatch to workers.")
	for _, f := range fields {
		if f.fixed != "" {
			add(f.label+": "+sc.value(f.key)+"   (fixed)", f.key, "Fixed: "+f.fixed)
		}
	}
	save := "Save and apply (nothing changed yet)"
	if n := len(sc.pending); n > 0 {
		save = "Save and apply (" + changes(n) + ")"
	}
	add(save, tagSave, "Writes the changes to the host_vars, says what applying them does, and applies them.")
	add("Discard changes", tagDiscard, "Forgets the changes not yet saved.")
	return rows, tags, headers
}

func (sc *screen) setting(key string) declare.Setting {
	at := slices.IndexFunc(sc.declared, func(s declare.Setting) bool { return s.Key == key })
	if at < 0 {
		return declare.Setting{Key: key}
	}
	return sc.declared[at]
}

// value is a field's value as shown: the pending one when it has been changed.
func (sc *screen) value(key string) string {
	if j, changed := sc.pending[key]; changed {
		return shownJSON(json.RawMessage(j))
	}
	return shown(sc.setting(key))
}

func (sc *screen) coordinatorName() string {
	if !sc.rosterOK {
		return "unknown (the members could not be read)"
	}
	if holder := sc.holder(); holder.Handle != "" {
		return holder.Named()
	}
	return none
}

// holder is the team's coordinator with what is known of its session; an empty Handle when
// the team has none.
func (sc *screen) holder() coordinator.Holder {
	return coordinator.Holding(sc.roster, "", sc.look)
}

// unreadable names the fields whose value ccy-teams cannot read; the entry is then not edited.
func (sc *screen) unreadable() string {
	var names []string
	for _, s := range sc.declared {
		if s.State != "" {
			names = append(names, fieldOf(s.Key).label)
		}
	}
	return strings.Join(names, ", ")
}

// editField asks for a field's new value until declare.py set --check accepts it with the other
// pending changes, maxTries at most; Esc leaves it as it was.
func (sc *screen) editField(ctx context.Context, f field) error {
	if f.fixed != "" {
		sc.notice = f.label + " cannot be changed here: " + f.fixed
		return nil
	}
	if names := sc.unreadable(); names != "" {
		sc.notice = "This team's entry has a value ccy-teams cannot read (vault-encrypted, or not in the form it " +
			"writes): " + names + ". Change the entry in the host_vars by hand."
		return nil
	}
	if err := sc.s.Kit.NeedGum(); err != nil {
		return err
	}
	original := inputJSON(f, editText(sc.setting(f.key).Value))
	value := editText(sc.setting(f.key).Value)
	if j, changed := sc.pending[f.key]; changed {
		value = editText(json.RawMessage(j))
	}
	for range maxTries {
		answer, err := sc.s.Kit.Gum.Input(ctx, sc.title(f.label)+"\n"+f.about, f.label, value)
		if errors.Is(err, ui.ErrCancelled) {
			sc.notice = f.label + " is as it was."
			return sc.s.Kit.Interrupted(ctx)
		}
		if err != nil {
			sc.s.Kit.ReportErr(err)
			return flow.Leave(term.ExitFailure)
		}
		j := inputJSON(f, answer)
		if j == original {
			delete(sc.pending, f.key)
			return nil
		}
		checkErr := sc.s.Declare.CheckSet(ctx, sc.team, sc.values(f.key, j))
		if checkErr == nil {
			sc.pending[f.key] = j
			return nil
		}
		sc.s.Kit.ReportErr(checkErr)
		if !errors.Is(checkErr, declare.ErrUsage) {
			return flow.Leave(term.ExitFailure)
		}
		value = answer
	}
	sc.notice = "Giving up after " + strconv.Itoa(maxTries) + " tries; " + f.label + " is as it was."
	return nil
}

// values are the pending changes, in field order, with key set to j when key is not empty.
func (sc *screen) values(key, j string) []declare.Value {
	var out []declare.Value
	for _, f := range fields {
		switch pending, changed := sc.pending[f.key]; {
		case f.key == key:
			out = append(out, declare.Value{Key: f.key, JSON: j})
		case changed:
			out = append(out, declare.Value{Key: f.key, JSON: pending})
		}
	}
	return out
}

// changes is n changes in words.
func changes(n int) string {
	if n == 1 {
		return "1 change"
	}
	return strconv.Itoa(n) + " changes"
}

// back leaves the screen, after asking when there are changes not saved.
func (sc *screen) back(ctx context.Context) (bool, error) {
	n := len(sc.pending)
	if n == 0 {
		return true, nil
	}
	verb := " are"
	if n == 1 {
		verb = " is"
	}
	yes, err := sc.s.Kit.Confirm(ctx, sc.title("Back"),
		changes(n)+verb+" not saved. Leave the settings and lose them?", "leave and lose them", false)
	if err != nil || yes {
		return true, err
	}
	return false, nil
}

// save shows what will change and what applying it does, then writes every change in one
// declare.py set and applies the team. Removing a person makes No the default.
func (sc *screen) save(ctx context.Context) (bool, flow.Result, error) {
	values := sc.values("", "")
	if len(values) == 0 {
		sc.notice = "Nothing has changed yet."
		return false, flow.Done, nil
	}
	removed, ready := sc.prepareSave(ctx)
	if !ready {
		return false, flow.Done, nil
	}
	yes, err := sc.s.Kit.Confirm(ctx, sc.title("Save and apply"), sc.summary(removed), "save and apply", len(removed) == 0)
	if err != nil {
		return true, flow.Done, err
	}
	if !yes {
		sc.notice = "Nothing was saved."
		return false, flow.Done, nil
	}
	var change declare.Change
	applied := false
	lockErr := sc.s.Locked(ctx, "save and apply team "+sc.team+"'s settings", func(ctx context.Context) error {
		change, err = sc.s.Declare.Set(ctx, sc.team, values)
		if err == nil && change.Changed {
			applied = sc.applied(ctx)
		}
		return nil
	})
	if errors.Is(lockErr, playlock.ErrNotTaken) {
		sc.notice = "Nothing was saved: the play lock was not free (above). Your changes are still here: " +
			"Save and apply once the other play run has finished."
		return false, flow.Done, nil
	}
	if lockErr != nil {
		sc.s.Kit.ReportErr(lockErr)
		return true, flow.Done, flow.Leave(term.ExitFailure)
	}
	if errors.Is(err, declare.ErrUsage) || errors.Is(err, declare.ErrRefused) {
		sc.notice = "Nothing was saved: " + err.Error()
		return false, flow.Done, nil
	}
	if err != nil {
		sc.s.Kit.ReportErr(err)
		return true, flow.Done, flow.Leave(term.ExitFailure)
	}
	sc.pending = map[string]string{}
	if !change.Changed {
		sc.notice = "Nothing to save: the host_vars already had these values."
		return false, flow.Done, nil
	}
	if !applied {
		sc.s.Kit.BackToList(ctx)
		return true, flow.Done, nil
	}
	res, err := sc.s.Kit.Note(ctx, "Team "+sc.team+"'s settings are saved and applied.")
	return true, res, err
}

// applied applies the saved team, and says so when it is saved but not applied (the apply has
// said why).
func (sc *screen) applied(ctx context.Context) bool {
	if sc.s.Apply(ctx, []string{sc.team}) == nil {
		return true
	}
	sc.s.Kit.Term.PrintError("team " + sc.team + "'s settings are saved in the host_vars but not applied. " +
		"Once the cause above is fixed, ccy-teams apply " + sc.team + " applies them.")
	return false
}

// summary is each change, old value to new, and what applying them does, each thing once.
func (sc *screen) summary(removed []string) string {
	lines := []string{"Written to the host_vars, then applied by play-agent-bus.yml for team " + sc.team + ":"}
	var applies []string
	for _, v := range sc.values("", "") {
		f := fieldOf(v.Key)
		lines = append(lines, "  "+f.label+": "+shown(sc.setting(v.Key))+" → "+shownJSON(json.RawMessage(v.JSON)))
		if !slices.Contains(applies, f.applies) {
			applies = append(applies, f.applies)
		}
	}
	lines = append(lines, "What applying does:")
	for _, a := range applies {
		lines = append(lines, "  "+a)
	}
	if len(removed) > 0 {
		lines = append(lines, "Removed, their account deactivated for good: "+strings.Join(removed, ", ")+". "+
			strings.Join(removed, ", ")+" can never be added back: a removed name never returns.")
	}
	return strings.Join(lines, "\n")
}

// prepareSave reads the team's settings again, so the summary's old values are those on disk
// now, and finds who the change removes; false, with the notice saying why, when it cannot.
func (sc *screen) prepareSave(ctx context.Context) ([]string, bool) {
	fresh, err := sc.s.Declare.Settings(ctx, sc.team)
	if err != nil {
		sc.notice = "Nothing was saved: the settings could not be read again: " + err.Error()
		return nil, false
	}
	sc.declared = fresh
	if names := sc.unreadable(); names != "" {
		sc.notice = "Nothing was saved: the entry now has a value ccy-teams cannot read: " + names + "."
		return nil, false
	}
	removed, err := sc.removedPeople()
	if err != nil {
		sc.notice = "Nothing was saved: " + err.Error()
		return nil, false
	}
	return removed, true
}

// removedPeople are the people the pending change takes off the team.
func (sc *screen) removedPeople() ([]string, error) {
	j, changed := sc.pending["humans"]
	if !changed {
		return nil, nil
	}
	var before, after []string
	if err := json.Unmarshal(sc.setting("humans").Value, &before); err != nil {
		return nil, fmt.Errorf("the people on disk are not a list of names: %w", err)
	}
	if err := json.Unmarshal([]byte(j), &after); err != nil {
		return nil, fmt.Errorf("the new people are not a list of names: %w", err)
	}
	var gone []string
	for _, name := range before {
		if !slices.Contains(after, name) {
			gone = append(gone, name)
		}
	}
	return gone, nil
}

// coordinator lists the team's members to make one the coordinator, or none; the change is
// made at once, by ChangeRole, which asks first.
func (sc *screen) coordinator(ctx context.Context) error {
	if !sc.rosterOK {
		sc.notice = "The members of " + sc.team + " could not be read (above), so the coordinator cannot be changed here."
		return nil
	}
	picked, err := sc.pickCoordinator(ctx)
	if errors.Is(err, ui.ErrLeave) {
		return sc.s.Kit.Interrupted(ctx)
	}
	if err != nil {
		return err
	}
	// One ChangeRole call either way: it asks the plain offer or the takeover itself (on No), and
	// a takeover's demote and promote happen inside it, with the holder put back if the promote
	// fails. Nothing here demotes on its own except No coordinator, which is that choice.
	holder := sc.holder()
	var changed bool
	said := picked + " is now the coordinator of " + sc.team + "."
	switch {
	case picked == tagNoOne:
		changed, err = sc.s.ChangeRole(ctx, sc.team, holder.Handle, false, sc.look)
		said = sc.team + " has no coordinator now; " + holder.Handle + " is a worker."
	case holder.Handle == picked:
		sc.notice = picked + " is already the coordinator of " + sc.team + "."
		return nil
	default:
		changed, err = sc.s.ChangeRole(ctx, sc.team, picked, true, sc.look)
	}
	switch {
	case err != nil:
		sc.notice = "Nothing was changed: " + err.Error()
	case !changed:
		sc.notice = "Nothing was changed."
	default:
		sc.notice = said
	}
	sc.readRoster(ctx)
	return nil
}

// pickCoordinator is the member chosen to coordinate, of any kind, or tagNoOne; ui.ErrLeave for
// Back. The two paths of package coordinator: a team with none is offered one plainly; a team
// with one is a takeover, which names it, says whether it runs and that it becomes a worker, and
// offers No coordinator, last, as it is destructive.
func (sc *screen) pickCoordinator(ctx context.Context) (string, error) {
	holder := sc.holder()
	rows := make([]string, 0, 1+len(sc.roster.Members))
	tags := make([]string, 0, 1+len(sc.roster.Members))
	about := sc.team + " has no coordinator. Choose the member to make its coordinator."
	if holder.Handle != "" {
		about = sc.team + "'s coordinator is " + holder.Named() + ". A team has one coordinator: choosing " +
			"another member is a takeover, " + holder.Handle + " becomes a worker, and the question starts on No."
	}
	for _, m := range sc.roster.Members {
		row := m.Handle
		if m.Handle == holder.Handle {
			row += "   (coordinator now)"
		}
		if m.Parked {
			row += "   (parked)"
		}
		rows, tags = append(rows, row), append(tags, m.Handle)
	}
	if holder.Handle != "" {
		rows, tags = append(rows, "No coordinator"), append(tags, tagNoOne)
	}
	picked, err := sc.s.Kit.Choose(ctx, ui.PickSpec{
		Title: sc.title("Coordinator"), Rows: rows, Tags: tags, ExitLabel: "Back to the settings",
		Header: ui.Header("Enter choose   Esc or q back to the settings",
			"Changed at once, not by Save and apply.", about),
	})
	return picked.Tag, err
}
