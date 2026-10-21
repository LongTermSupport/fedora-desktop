package proto

import (
	"maps"
	"slices"
)

// The roster's state event (A 5.1).
const (
	RosterType = "teamchat.roster"
	// WireMajor is the one major of this design (A 3.1).
	WireMajor = 1
)

// The member roles of A 5.1. v2 writes and reads `coordinator` only: no `orchestrator` alias.
const (
	RoleCoordinator = "coordinator"
	RoleWorker      = "worker"
	RoleProbe       = "probe"
)

// The bounds of A 5.1 and 11.2.
const (
	maxHumans    = 16
	maxMembers   = 64
	maxRepos     = 32
	maxBranches  = 8
	maxExclude   = 32
	maxPrefixes  = 32
	maxEpoch     = 999_999_999
	maxRosterRev = maxSafeInt
)

// RosterError is a roster the protocol refuses, which makes the team untrusted. Rule names the
// key at fault and never echoes a value (A 2.2).
type RosterError struct {
	Rule string
}

func (e *RosterError) Error() string { return "teamchat roster invalid: " + e.Rule }

// Roster is the decoded `teamchat.roster` content (A 5.1). Its `v` is WireMajor.
type Roster struct {
	Team          string
	Name          string
	Rev           int64
	RestoreEpoch  int64
	QuestionFloor int64
	Humans        []string
	Members       map[string]RosterMember
	Rooms         SideRooms
	Forge         Forge
	Limits        Limits
}

// RosterMember is one agent's entry: its role, whether it reads human text (default true) and
// whether it is parked (default false).
type RosterMember struct {
	Role      string
	HumanText bool
	Parked    bool
}

// SideRooms are the room IDs of the reports and questions rooms.
type SideRooms struct {
	Reports, Questions string
}

// Forge is the roster's `forge` section: the allow-list of A 9.2. C decodes a declaration's
// `trust` with DecodeForge (seam row 16).
type Forge struct {
	Kind         string
	Org          string
	Repos        []ForgeRepo
	Exclude      []string
	PathPrefixes []string
}

// ForgeRepo is a listed repository; nil Branches means any branch proves it.
type ForgeRepo struct {
	Repo     string
	Branches []string
}

// DecodeRoster decodes and validates a roster for this server, team and main room. Unknown keys
// are ignored; `features` present at all is refused (A 3.1): v2 defines none.
func DecodeRoster(v Value, server, team, mainRoom string) (Roster, error) {
	f := readFields(v, "shape")
	if f.rule == "" && f.integer("v", true, 0, "version") != WireMajor {
		f.fail("version")
	}
	if f.has("features") {
		f.fail("features")
	}
	r := Roster{
		Team:          f.str("team", true, "shape"),
		Name:          f.str("name", true, "shape"),
		Rev:           f.integer("rev", true, 0, "shape"),
		RestoreEpoch:  f.integer("restore_epoch", false, 0, "shape"),
		QuestionFloor: f.integer("question_floor", false, 0, "shape"),
		Humans:        f.strs("humans", true, "shape"),
		Members:       decodeMembers(f, f.need("members", "shape")),
		Rooms:         decodeRooms(f, f.need("rooms", "shape")),
		Limits:        DefaultLimits(),
	}
	forge, err := decodeForgeShape(f.need("forge", "shape"))
	if f.rule != "" {
		return Roster{}, &RosterError{Rule: f.rule}
	}
	if err != nil {
		return Roster{}, err
	}
	r.Forge = forge
	if limits, ok := f.object("limits", false, "shape"); ok {
		if r.Limits, err = DecodeLimits(limits); err != nil {
			return Roster{}, err
		}
	}
	if f.rule != "" {
		return Roster{}, &RosterError{Rule: f.rule}
	}
	return r, r.Validate(server, team, mainRoom)
}

func decodeMembers(f *fields, obj Value) map[string]RosterMember {
	members := make(map[string]RosterMember, len(obj.members))
	for _, m := range obj.members {
		mf := readFields(m.Val, "shape")
		members[m.Key] = RosterMember{
			Role:      mf.str("role", true, "shape"),
			HumanText: mf.boolean("human_text", true, "shape"),
			Parked:    mf.boolean("parked", false, "shape"),
		}
		f.fail(mf.rule)
	}
	return members
}

func decodeRooms(f *fields, obj Value) SideRooms {
	if obj.kind != KindObject {
		return SideRooms{}
	}
	rf := readFields(obj, "shape")
	rooms := SideRooms{Reports: rf.str("reports", true, "shape"), Questions: rf.str("questions", true, "shape")}
	f.fail(rf.rule)
	return rooms
}

// DecodeForge decodes and validates a `forge` section on its own.
func DecodeForge(v Value) (Forge, error) {
	f, err := decodeForgeShape(v)
	if err != nil {
		return Forge{}, err
	}
	return f, f.Validate()
}

func decodeForgeShape(v Value) (Forge, error) {
	f := readFields(v, "shape")
	forge := Forge{
		Kind:         f.str("kind", true, "shape"),
		Org:          f.str("org", false, "shape"),
		Exclude:      f.strs("exclude", false, "shape"),
		PathPrefixes: f.strs("path_prefixes", false, "shape"),
	}
	if repos, ok := f.get("repos", KindArray, false, "shape"); ok {
		forge.Repos = make([]ForgeRepo, 0, len(repos.items))
		for _, item := range repos.items {
			forge.Repos = append(forge.Repos, decodeForgeRepo(f, item))
		}
	}
	if f.rule != "" {
		return Forge{}, &RosterError{Rule: f.rule}
	}
	// An empty list is written as none (A 5.1's example writes `"path_prefixes": []`).
	if len(forge.Exclude) == 0 {
		forge.Exclude = nil
	}
	if len(forge.PathPrefixes) == 0 {
		forge.PathPrefixes = nil
	}
	return forge, nil
}

// decodeForgeRepo is one `repos` entry: `repo` and optional `branches`, nothing else (v1's
// exact entry shape).
func decodeForgeRepo(f *fields, item Value) ForgeRepo {
	rf := readFields(item, "shape")
	repo := ForgeRepo{Repo: rf.str("repo", true, "shape"), Branches: rf.strs("branches", false, "shape")}
	for _, m := range item.members {
		if m.Key != "repo" && m.Key != "branches" {
			rf.fail("shape")
		}
	}
	f.fail(rf.rule)
	return repo
}

// Validate holds the roster to every rule of A 5.1 and the list counts of 11.2.
func (r Roster) Validate(server, team, mainRoom string) error {
	checks := []struct {
		ok   bool
		rule string
	}{
		{ValidTeamID(team) && r.Team == team, "team"},
		{ValidDisplayName(r.Name), "name"},
		{r.Rev >= 1 && r.Rev <= maxRosterRev, "rev"},
		{r.RestoreEpoch >= 0 && r.RestoreEpoch <= maxEpoch, "restore_epoch"},
		{r.QuestionFloor >= 0 && r.QuestionFloor <= maxEpoch, "question_floor"},
		{r.validHumans(server), "humans"},
		{r.validMembers(server), "members"},
		{validSideRooms(r.Rooms, mainRoom), "rooms"},
	}
	for _, c := range checks {
		if !c.ok {
			return &RosterError{Rule: c.rule}
		}
	}
	if err := r.Forge.Validate(); err != nil {
		return err
	}
	return r.Limits.Validate()
}

func validSideRooms(rooms SideRooms, mainRoom string) bool {
	return ValidRoomID(rooms.Reports) && ValidRoomID(rooms.Questions) && rooms.Reports != rooms.Questions &&
		rooms.Reports != mainRoom && rooms.Questions != mainRoom
}

// distinctList is a list of low to high distinct values, each valid.
func distinctList(list []string, low, high int, valid func(string) bool) bool {
	if len(list) < low || len(list) > high {
		return false
	}
	seen := make(map[string]bool, len(list))
	for _, s := range list {
		if seen[s] || !valid(s) {
			return false
		}
		seen[s] = true
	}
	return true
}

// Validate holds the forge section to A 5.1: kind github, at least one of org or a repo, the
// list counts of 11.2, and no excluded repository also listed.
func (f Forge) Validate() error {
	ok := f.Kind == ForgeGitHub &&
		(f.Org == "" || ValidOwner(f.Org)) &&
		(f.Org != "" || len(f.Repos) > 0) &&
		(f.Exclude == nil || distinctList(f.Exclude, 1, maxExclude, ValidOwnerRepo)) &&
		(f.PathPrefixes == nil || distinctList(f.PathPrefixes, 1, maxPrefixes, ValidPathPrefix))
	names := make([]string, 0, len(f.Repos))
	for _, repo := range f.Repos {
		ok = ok && (repo.Branches == nil || distinctList(repo.Branches, 1, maxBranches, ValidBranch)) &&
			!slices.Contains(f.Exclude, repo.Repo)
		names = append(names, repo.Repo)
	}
	if !ok || !distinctList(names, 0, maxRepos, ValidOwnerRepo) {
		return &RosterError{Rule: "forge"}
	}
	return nil
}

// Encode is the roster's content: every key in A 5.1's order, members sorted by user ID,
// `human_text` written only when false and `parked` only when true.
func (r Roster) Encode() Value {
	ids := slices.Sorted(maps.Keys(r.Members))
	members := make([]Member, 0, len(ids))
	for _, id := range ids {
		m := r.Members[id]
		entry := []Member{M("role", Str(m.Role))}
		if !m.HumanText {
			entry = append(entry, M("human_text", Bool(false)))
		}
		if m.Parked {
			entry = append(entry, M("parked", Bool(true)))
		}
		members = append(members, M(id, Obj(entry...)))
	}
	return Obj(
		M("v", Int(WireMajor)), M("team", Str(r.Team)), M("name", Str(r.Name)), M("rev", Int(r.Rev)),
		M("restore_epoch", Int(r.RestoreEpoch)), M("question_floor", Int(r.QuestionFloor)),
		M("humans", strArray(r.Humans)), M("members", Obj(members...)),
		M("rooms", Obj(M("reports", Str(r.Rooms.Reports)), M("questions", Str(r.Rooms.Questions)))),
		M("forge", r.Forge.Encode()), M("limits", r.Limits.Encode()),
	)
}

// Encode is the forge section; `org`, `exclude` and `path_prefixes` only when set.
func (f Forge) Encode() Value {
	repos := make([]Value, 0, len(f.Repos))
	for _, repo := range f.Repos {
		entry := []Member{M("repo", Str(repo.Repo))}
		if repo.Branches != nil {
			entry = append(entry, M("branches", strArray(repo.Branches)))
		}
		repos = append(repos, Obj(entry...))
	}
	members := []Member{M("kind", Str(f.Kind))}
	if f.Org != "" {
		members = append(members, M("org", Str(f.Org)))
	}
	members = append(members, M("repos", Arr(repos...)))
	if f.Exclude != nil {
		members = append(members, M("exclude", strArray(f.Exclude)))
	}
	if f.PathPrefixes != nil {
		members = append(members, M("path_prefixes", strArray(f.PathPrefixes)))
	}
	return Obj(members...)
}

func strArray(list []string) Value {
	items := make([]Value, 0, len(list))
	for _, s := range list {
		items = append(items, Str(s))
	}
	return Arr(items...)
}

// SameExceptRev is whether two rosters differ in nothing but `rev`, the comparison C makes
// before it writes (C 3.6.1).
func (r Roster) SameExceptRev(o Roster) bool {
	o.Rev = r.Rev
	return r.Encode().Equal(o.Encode())
}

// NextRev is the `rev` of the next write: one above the larger of the event's and C's file's
// (seam row 17).
func NextRev(eventRev, fileRev int64) int64 { return max(eventRev, fileRev) + 1 }

// SenderClass is a sender's class (A 6.7).
type SenderClass int

// The sender classes of A 6.7.
const (
	SenderNone SenderClass = iota
	SenderAdmin
	SenderHuman
	SenderCoordinator
	SenderWorker
	SenderProbe
)

// Sender is the class of a sender: the admin, a listed human, or a non-parked member by its
// role; anyone else, parked members included, is none.
func (r Roster) Sender(id, server string) SenderClass {
	switch {
	case id == AdminID(server):
		return SenderAdmin
	case r.IsHuman(id):
		return SenderHuman
	}
	m, ok := r.Members[id]
	if !ok || m.Parked {
		return SenderNone
	}
	switch m.Role {
	case RoleCoordinator:
		return SenderCoordinator
	case RoleWorker:
		return SenderWorker
	case RoleProbe:
		return SenderProbe
	}
	return SenderNone
}

// IsHuman is whether id is a listed human.
func (r Roster) IsHuman(id string) bool { return slices.Contains(r.Humans, id) }

// AddressableAgent is a non-parked coordinator or worker (A 8.1).
func (r Roster) AddressableAgent(id string) bool {
	m, ok := r.Members[id]
	return ok && !m.Parked && (m.Role == RoleCoordinator || m.Role == RoleWorker)
}

// HumanText is whether a member reads human text: false for anyone not a member.
func (r Roster) HumanText(id string) bool {
	m, ok := r.Members[id]
	return ok && m.HumanText
}

// Coordinator is the non-parked coordinator, if any.
func (r Roster) Coordinator() (string, bool) {
	for id, m := range r.Members {
		if m.Role == RoleCoordinator && !m.Parked {
			return id, true
		}
	}
	return "", false
}

func (r Roster) validHumans(server string) bool {
	return distinctList(r.Humans, 1, maxHumans, func(id string) bool { return ClassifyUserID(id, server) == IDHuman })
}

// validMembers: every key an agent of this server; the probe role exactly on the probe handle,
// at most one; at most one non-parked coordinator, and no coordinator without human text.
func (r Roster) validMembers(server string) bool {
	if len(r.Members) > maxMembers {
		return false
	}
	coordinators, probes := 0, 0
	for id, m := range r.Members {
		h, ok := AgentHandle(id, server)
		if !ok || h.IsProbe() != (m.Role == RoleProbe) {
			return false
		}
		switch m.Role {
		case RoleCoordinator:
			if !m.HumanText {
				return false
			}
			if !m.Parked {
				coordinators++
			}
		case RoleProbe:
			probes++
		case RoleWorker:
		default:
			return false
		}
	}
	return coordinators <= 1 && probes <= 1
}
