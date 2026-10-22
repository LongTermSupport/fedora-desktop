package declare

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	statefile "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// FieldTeam is the gate's name for a team's existence (AG1, AG6).
const FieldTeam = keyTeam

// gatedFields are the security fields of AG1, in the order AG3 lists their differences.
var gatedFields = []string{keyHumans, keyCoordinator, keyAllowFrom, keyListen, keyURL, keyTrust, keyLXCMembers}

// none is a difference's side that has no value (AG3).
const none = "-"

// maxApproved caps `approved.json` (AG2).
const maxApproved = 256 << 10

// approvedFileMode is `approved.json`'s mode: the owner group reads it (AG7).
const approvedFileMode = 0o640

const (
	keyApproved     = "approved"
	humanTextMarker = ",human_text="
)

// LXCEntry is an LXC member as the gate sees it (AG1): who it is, and whether it may talk to
// the humans.
type LXCEntry struct {
	Container string
	User      paths.GuestUser
	HumanText bool
}

// String is the entry as AG3 prints it and AG6 takes it: `<container>/<user>,human_text=<bool>`.
func (e LXCEntry) String() string {
	return e.who() + humanTextMarker + strconv.FormatBool(e.HumanText)
}

func (e LXCEntry) who() string { return e.Container + "/" + e.User.String() }

// ParseLXCEntry reads an entry in exactly the form String prints.
func ParseLXCEntry(s string) (LXCEntry, error) {
	who, flag, found := strings.Cut(s, humanTextMarker)
	container, user, slash := strings.Cut(who, "/")
	bad := errors.New("lxc_members: the entry is not <container>/<user>,human_text=<true|false>")
	if !found || !slash || (flag != "true" && flag != "false") || !validContainer(container) {
		return LXCEntry{}, bad
	}
	u, err := paths.ParseGuestUser(user)
	if err != nil || u.String() != user {
		return LXCEntry{}, bad
	}
	return LXCEntry{Container: container, User: u, HumanText: flag == "true"}, nil
}

// Gated is a team's security fields (AG1). The lists are sorted, so two values compare as sets.
type Gated struct {
	Humans []string
	// Coordinator is empty when none is declared.
	Coordinator string
	AllowFrom   []string
	Listen      []string
	// URL is the resolved URL (n2b-D2).
	URL   string
	Trust proto.Forge
	LXC   []LXCEntry
}

// Gated is the input's security fields; every LXC entry of the team counts, whatever its state.
func (in Input) Gated() Gated {
	g := Gated{
		Humans: sorted(in.Team.Humans), Coordinator: in.Team.Coordinator, AllowFrom: sorted(in.Team.AllowFrom),
		Listen: sorted(in.Team.Listen), URL: in.Team.URL, Trust: in.Team.Trust,
	}
	for _, m := range in.LXC {
		g.LXC = append(g.LXC, LXCEntry{Container: m.Container, User: m.User, HumanText: m.HumanText})
	}
	sortLXC(g.LXC)
	return g
}

func sorted(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

func sortLXC(list []LXCEntry) {
	slices.SortFunc(list, func(a, b LXCEntry) int { return strings.Compare(a.String(), b.String()) })
}

func (g Gated) clone() Gated {
	g.Humans, g.AllowFrom, g.Listen = slices.Clone(g.Humans), slices.Clone(g.AllowFrom), slices.Clone(g.Listen)
	g.LXC = slices.Clone(g.LXC)
	return g
}

// listField is a field compared and approved as a set of entries (n2b-D3).
func listField(field string) bool {
	return field == keyHumans || field == keyAllowFrom || field == keyListen || field == keyLXCMembers
}

// entries is a list field's entries as AG3 prints them.
func (g Gated) entries(field string) []string {
	switch field {
	case keyHumans:
		return g.Humans
	case keyAllowFrom:
		return g.AllowFrom
	case keyListen:
		return g.Listen
	}
	out := make([]string, 0, len(g.LXC))
	for _, e := range g.LXC {
		out = append(out, e.String())
	}
	return out
}

// scalar is a scalar field's value as AG3 prints it: `-` for no coordinator, trust as compact
// JSON.
func (g Gated) scalar(field string) string {
	switch field {
	case keyCoordinator:
		if g.Coordinator == "" {
			return none
		}
		return g.Coordinator
	case keyURL:
		return g.URL
	}
	return string(g.Trust.Encode().Compact())
}

// Difference is one change between the approved fields and the declared ones (AG3).
type Difference struct {
	Field, Old, New string
}

// Line is the difference as `declare check` prints it: `SECURITY<TAB>team<TAB>field<TAB>old -> new`.
func (d Difference) Line(team paths.TeamID) string {
	return "SECURITY\t" + team.String() + "\t" + d.Field + "\t" + d.Old + " -> " + d.New
}

// Compare lists every difference between approved (nil: never approved) and the input (AG3).
func Compare(approved *Gated, in Input) []Difference {
	if approved == nil {
		return []Difference{{Field: FieldTeam, Old: none, New: "new"}}
	}
	return diff(*approved, in.Gated())
}

func diff(old, cur Gated) []Difference {
	var out []Difference
	for _, field := range gatedFields {
		if listField(field) {
			out = append(out, setDiff(field, old.entries(field), cur.entries(field))...)
			continue
		}
		if was, is := old.scalar(field), cur.scalar(field); was != is {
			out = append(out, Difference{Field: field, Old: was, New: is})
		}
	}
	return out
}

// setDiff is the removed entries, then the added ones, each in sorted order.
func setDiff(field string, was, is []string) []Difference {
	var out []Difference
	for _, e := range was {
		if !slices.Contains(is, e) {
			out = append(out, Difference{Field: field, Old: e, New: none})
		}
	}
	for _, e := range is {
		if !slices.Contains(was, e) {
			out = append(out, Difference{Field: field, Old: none, New: e})
		}
	}
	return out
}

// Approvals is where a team's approved fields are kept; Load answers nil for a team never
// approved.
type Approvals interface {
	Load(team paths.TeamID) (*Gated, error)
}

// Check is the SECURITY lines of every present team, in declaration order (AG4). An absent
// team is not compared (n2b-D4).
func Check(d Declaration, approvals Approvals) ([]string, error) {
	var lines []string
	for _, in := range d.Inputs() {
		approved, err := approvals.Load(in.ID())
		if err != nil {
			return nil, err
		}
		for _, diff := range Compare(approved, in) {
			lines = append(lines, diff.Line(in.ID()))
		}
	}
	return lines, nil
}

// RefusedError is an input the gate refuses, or an approve it will not write (exit 70).
type RefusedError struct {
	Team        paths.TeamID
	Problem     string
	Differences []Difference
}

func (e *RefusedError) Error() string {
	var b strings.Builder
	b.WriteString("team " + e.Team.String() + ": " + e.Problem)
	for _, d := range e.Differences {
		b.WriteString("\n" + d.Line(e.Team))
	}
	b.WriteString("\nthe owner approves with: ccy-teams approve " + e.Team.String())
	return b.String()
}

// Hold refuses an input whose security fields are not the approved ones (AG4).
func Hold(approved *Gated, in Input) error {
	if diffs := Compare(approved, in); len(diffs) > 0 {
		return &RefusedError{Team: in.ID(), Problem: "the declaration's security fields are not approved", Differences: diffs}
	}
	return nil
}

// Op is how a one-field approve changes the field (AG6).
type Op string

// The approve operations: --add, --remove and --set.
const (
	OpAdd    Op = "add"
	OpRemove Op = "remove"
	OpSet    Op = "set"
)

// Change is one `team approve --only <Field> --<Op> <Value>`.
type Change struct {
	Field string
	Op    Op
	Value string
}

// Approve is what `team approve` writes (AG5, AG6): with no change, the input's fields; with
// one, the approved fields with that change applied, and only when the input's fields equal
// them in every field, so nothing planted beside the change is approved.
func Approve(approved *Gated, in Input, ch *Change) (Gated, error) {
	cur := in.Gated()
	if ch == nil {
		return cur, nil
	}
	expected, err := applyChange(approved, *ch)
	if err != nil {
		return Gated{}, &RefusedError{Team: in.ID(), Problem: err.Error()}
	}
	if diffs := diff(expected, cur); len(diffs) > 0 {
		return Gated{}, &RefusedError{
			Team: in.ID(), Problem: "the declaration is not the approved fields with this change alone", Differences: diffs,
		}
	}
	return expected, nil
}

func applyChange(approved *Gated, ch Change) (Gated, error) {
	if ch.Field == FieldTeam {
		if approved != nil {
			return Gated{}, errors.New("the team is approved already; approve one field of it, or the whole declaration")
		}
		if ch.Op != OpSet {
			return Gated{}, errors.New("team takes --set with the approved object")
		}
		return parseApprovedObject(ch.Value)
	}
	if approved == nil {
		return Gated{}, errors.New("the team is not approved yet: approve it whole, or with --only team")
	}
	g := approved.clone()
	var err error
	switch ch.Field {
	case keyHumans:
		g.Humans, err = changeSet(g.Humans, ch, proto.ValidHumanName)
	case keyAllowFrom:
		g.AllowFrom, err = changeSet(g.AllowFrom, ch, validPrefix)
	case keyListen:
		g.Listen, err = changeSet(g.Listen, ch, validAddress)
	case keyLXCMembers:
		g.LXC, err = changeLXC(g.LXC, ch)
	case keyCoordinator:
		g.Coordinator, err = changeCoordinator(g.Coordinator, ch)
	case keyURL:
		g.URL, err = changeScalar(g.URL, ch, validURL)
	case keyTrust:
		g.Trust, err = changeTrust(g.Trust, ch)
	default:
		err = fmt.Errorf("%q is not a field the gate approves", ch.Field)
	}
	return g, err
}

func noChange(field string) error {
	return fmt.Errorf("%s: the change changes nothing", field)
}

func badValue(field string) error {
	return fmt.Errorf("%s: the value is not valid for the field", field)
}

func badOp(field, ops string) error {
	return fmt.Errorf("%s takes %s", field, ops)
}

// changeSet adds or removes one entry of a list field.
func changeSet(list []string, ch Change, valid func(string) bool) ([]string, error) {
	if ch.Op != OpAdd && ch.Op != OpRemove {
		return nil, badOp(ch.Field, "--add or --remove")
	}
	if !valid(ch.Value) {
		return nil, badValue(ch.Field)
	}
	at := slices.Index(list, ch.Value)
	switch {
	case ch.Op == OpAdd && at < 0:
		return sorted(append(list, ch.Value)), nil
	case ch.Op == OpRemove && at >= 0:
		return slices.Delete(list, at, at+1), nil
	}
	return nil, noChange(ch.Field)
}

// changeLXC adds or removes an entry, or sets the human_text of the entry with that container
// and user.
func changeLXC(list []LXCEntry, ch Change) ([]LXCEntry, error) {
	e, err := ParseLXCEntry(ch.Value)
	if err != nil {
		return nil, err
	}
	at := slices.IndexFunc(list, func(x LXCEntry) bool { return x.who() == e.who() })
	switch {
	case ch.Op == OpAdd && at < 0:
		list = append(list, e)
	case ch.Op == OpRemove && at >= 0 && list[at] == e:
		list = slices.Delete(list, at, at+1)
	case ch.Op == OpSet && at >= 0 && list[at] != e:
		list[at] = e
	case ch.Op != OpAdd && ch.Op != OpRemove && ch.Op != OpSet:
		return nil, badOp(ch.Field, "--add, --remove or --set")
	default:
		return nil, noChange(ch.Field)
	}
	sortLXC(list)
	return list, nil
}

// changeCoordinator sets the coordinator, or removes the one named.
func changeCoordinator(cur string, ch Change) (string, error) {
	if !validHandle(ch.Value) {
		return "", badValue(ch.Field)
	}
	switch {
	case ch.Op == OpSet && ch.Value != cur:
		return ch.Value, nil
	case ch.Op == OpRemove && ch.Value == cur:
		return "", nil
	case ch.Op != OpSet && ch.Op != OpRemove:
		return "", badOp(ch.Field, "--set or --remove")
	}
	return "", noChange(ch.Field)
}

func changeScalar(cur string, ch Change, valid func(string) bool) (string, error) {
	if ch.Op != OpSet {
		return "", badOp(ch.Field, "--set")
	}
	if !valid(ch.Value) {
		return "", badValue(ch.Field)
	}
	if ch.Value == cur {
		return "", noChange(ch.Field)
	}
	return ch.Value, nil
}

func changeTrust(cur proto.Forge, ch Change) (proto.Forge, error) {
	if ch.Op != OpSet {
		return proto.Forge{}, badOp(ch.Field, "--set")
	}
	v, err := proto.Parse([]byte(ch.Value))
	if err != nil {
		return proto.Forge{}, badValue(ch.Field)
	}
	f, err := node{path: keyTrust, v: v}.trust()
	if err != nil {
		return proto.Forge{}, err
	}
	if string(f.Encode().Compact()) == string(cur.Encode().Compact()) {
		return proto.Forge{}, noChange(ch.Field)
	}
	return f, nil
}

// value is the `approved` object of AG2.
func (g Gated) value() proto.Value {
	coordinator := proto.Null()
	if g.Coordinator != "" {
		coordinator = proto.Str(g.Coordinator)
	}
	lxc := make([]proto.Value, 0, len(g.LXC))
	for _, e := range g.LXC {
		lxc = append(lxc, proto.Obj(
			proto.M(keyContainer, proto.Str(e.Container)), proto.M(keyUser, proto.Str(e.User.String())),
			proto.M(keyHumanText, proto.Bool(e.HumanText))))
	}
	return proto.Obj(
		proto.M(keyHumans, strArray(g.Humans)), proto.M(keyCoordinator, coordinator),
		proto.M(keyAllowFrom, strArray(g.AllowFrom)), proto.M(keyListen, strArray(g.Listen)),
		proto.M(keyURL, proto.Str(g.URL)), proto.M(keyTrust, g.Trust.Encode()), proto.M(keyLXCMembers, proto.Arr(lxc...)))
}

// EncodeApproved is a team's `approved.json` (AG2).
func EncodeApproved(team paths.TeamID, g Gated) []byte {
	return proto.Obj(
		proto.M(keyVersion, proto.Int(teamFileVersion)), proto.M(keyTeam, proto.Str(team.String())),
		proto.M(keyApproved, g.value())).Compact()
}

// DecodeApproved reads team's `approved.json` strictly (AG2).
func DecodeApproved(raw []byte, team paths.TeamID) (Gated, error) {
	if len(raw) > maxApproved {
		return Gated{}, fmt.Errorf("approved.json: over %d bytes", maxApproved)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return Gated{}, fmt.Errorf("approved.json: %w", err)
	}
	f, err := node{path: "approved.json", v: v}.object([]string{keyVersion, keyTeam, keyApproved})
	if err != nil {
		return Gated{}, err
	}
	if ver, verErr := f.at(keyVersion).integer(); verErr != nil || ver != teamFileVersion {
		return Gated{}, f.at(keyVersion).fail("is not 1")
	}
	id, err := f.at(keyTeam).str()
	if err != nil {
		return Gated{}, err
	}
	if id != team.String() {
		return Gated{}, f.at(keyTeam).fail("is not the team asked for")
	}
	return readGated(f.at(keyApproved))
}

// parseApprovedObject reads `--only team --set`'s value: the `approved` object alone.
func parseApprovedObject(raw string) (Gated, error) {
	v, err := proto.Parse([]byte(raw))
	if err != nil {
		return Gated{}, fmt.Errorf("team: the value is not JSON: %w", err)
	}
	return readGated(node{path: keyApproved, v: v})
}

// readGated reads the `approved` object by the declaration's value rules.
func readGated(n node) (Gated, error) {
	f, err := n.object(gatedFields)
	if err != nil {
		return Gated{}, err
	}
	var g Gated
	if g.Humans, err = f.at(keyHumans).humans(); err != nil {
		return Gated{}, err
	}
	if c := f.at(keyCoordinator); c.v.Kind() != proto.KindNull {
		if g.Coordinator, err = c.coordinator(); err != nil {
			return Gated{}, err
		}
	}
	if g.AllowFrom, err = f.at(keyAllowFrom).allowFrom(); err != nil {
		return Gated{}, err
	}
	if g.Listen, err = f.at(keyListen).listen(); err != nil {
		return Gated{}, err
	}
	if g.URL, err = f.at(keyURL).url(); err != nil {
		return Gated{}, err
	}
	if g.Trust, err = f.at(keyTrust).trust(); err != nil {
		return Gated{}, err
	}
	if g.LXC, err = readLXCEntries(f.at(keyLXCMembers)); err != nil {
		return Gated{}, err
	}
	g.Humans, g.AllowFrom, g.Listen = sorted(g.Humans), sorted(g.AllowFrom), sorted(g.Listen)
	return g, nil
}

func readLXCEntries(n node) ([]LXCEntry, error) {
	items, err := n.list()
	if err != nil {
		return nil, err
	}
	out := make([]LXCEntry, 0, len(items))
	for _, item := range items {
		f, err := item.object([]string{keyContainer, keyUser, keyHumanText})
		if err != nil {
			return nil, err
		}
		var e LXCEntry
		if e.Container, err = f.at(keyContainer).checked(validContainer, "is not a container name"); err != nil {
			return nil, err
		}
		if e.User, err = guestUser(f.at(keyUser)); err != nil {
			return nil, err
		}
		if e.HumanText, err = f.at(keyHumanText).boolean(); err != nil {
			return nil, err
		}
		if slices.ContainsFunc(out, func(x LXCEntry) bool { return x.who() == e.who() }) {
			return nil, item.fail("repeats another entry's container and user")
		}
		out = append(out, e)
	}
	sortLXC(out)
	return out, nil
}

// FileApprovals keeps each team's `approved.json` at Path(team), group GID (AG7).
type FileApprovals struct {
	Path func(paths.TeamID) string
	GID  int
}

// Load is team's approved fields; nil when the team has no file.
func (a FileApprovals) Load(team paths.TeamID) (*Gated, error) {
	raw, found, err := statefile.ReadFile(a.Path(team), maxApproved)
	if err != nil || !found {
		return nil, err
	}
	g, err := DecodeApproved(raw, team)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Save writes team's approved fields.
func (a FileApprovals) Save(team paths.TeamID, g Gated) error {
	return statefile.WriteFile(a.Path(team), EncodeApproved(team, g), approvedFileMode, a.GID)
}
