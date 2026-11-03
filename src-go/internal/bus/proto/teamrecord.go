package proto

import (
	"slices"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The state event types of §8 and §8.1.
const (
	TypeTeam     = "agent_bus.team"
	TypeStatus   = "agent_bus.status"
	TypeRooms    = "agent_bus.rooms"
	TypeRoomLink = "agent_bus.room"
)

// The team record's limits (§8).
const (
	humansMax       = 16
	rolesMax        = 64
	reposMax        = 32
	branchesMax     = 8
	pathPrefixesMax = 32
	excludeMax      = reposMax
	maxStatusBytes  = 256
	humanPower      = 50
	closedPower     = 100
	tombstonePower  = 150
	statusListening = "listening"
	reasonListSize  = "list size or duplicates"
	reasonReposItem = "repos entry"
)

var (
	teamRequiredKeys = []string{"v", "team", "humans", "roles", "repos", "forge_api"}
	teamOptionalKeys = []string{"org", "exclude", "path_prefixes"}
	statusKeys       = []string{"v", "state", "until"}
	forgeAPIRE       = whole(`https://[^/` + pySpaceClass + `]+(?:/[^` + pySpaceClass + `]*)?`)
)

// TeamRecord is a verified team record (SPEC P43). Humans is sorted; Roles maps agent user IDs
// to coordinator or worker; Repos maps OWNER/REPO to its trusted branches, none for any
// branch; Org is "" when the team names none.
type TeamRecord struct {
	Team         string
	Humans       []string
	Roles        map[string]string
	Repos        map[string][]string
	PathPrefixes []string
	ForgeAPI     string
	Org          string
	Exclude      []string
}

// Context is what validation needs from this record on the given server.
func (t *TeamRecord) Context(serverName string) *Context {
	return &Context{
		ServerName: serverName, Humans: slices.Clone(t.Humans), Roles: t.Roles, Repos: t.Repos,
		PathPrefixes: slices.Clone(t.PathPrefixes), Org: t.Org, Exclude: slices.Clone(t.Exclude),
	}
}

func distrust(reason string) error { return &UntrustedError{Reason: reason} }

func intNum(n int64) pyjson.Number {
	return pyjson.Number{Lit: strconv.FormatInt(n, 10), Kind: pyjson.Int}
}

// isIntOne is Python's `type(v) is int and v == 1`.
func isIntOne(v any) bool {
	n, ok := v.(pyjson.Number)
	return ok && n.IsInt(1)
}

// hasExactKeys is set(obj) == set(keys).
func hasExactKeys(obj pyjson.Object, keys ...string) bool {
	if len(obj) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	return true
}

func teamKeysOK(obj pyjson.Object) bool {
	for _, k := range teamRequiredKeys {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	for k := range obj {
		if !slices.Contains(teamRequiredKeys, k) && !slices.Contains(teamOptionalKeys, k) {
			return false
		}
	}
	return true
}

// listOf is a list of low to high entries, distinct by their canonical JSON (SPEC P2).
func listOf(v any, low, high int) ([]any, error) {
	items, ok := v.([]any)
	if !ok || len(items) < low || len(items) > high {
		return nil, distrust(reasonListSize)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		key := pyjson.Canonical(item)
		if seen[key] {
			return nil, distrust(reasonListSize)
		}
		seen[key] = true
	}
	return items, nil
}

// stringsWhere is the entries as strings when every one is a string the predicate accepts.
func stringsWhere(items []any, pred func(string) bool) ([]string, bool) {
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok || !pred(s) {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// ParseTeamRecord is SPEC P43: the content of an agent_bus.team event, or an UntrustedError
// naming the first deviation.
func ParseTeamRecord(content any, serverName, teamName string) (*TeamRecord, error) {
	obj, ok := content.(pyjson.Object)
	if !ok || !teamKeysOK(obj) {
		return nil, distrust("team record keys")
	}
	if !isIntOne(obj["v"]) {
		return nil, distrust("team record version")
	}
	if team, isStr := obj["team"].(string); !isStr || !IsTeamName(team) || team != teamName {
		return nil, distrust("team name")
	}
	rec := &TeamRecord{Team: teamName}
	for _, step := range []func(pyjson.Object, *TeamRecord, string) error{
		readHumans, readRoles, readPathPrefixes, readForgeAPI, readOrg, readRepos, readExclude,
	} {
		if err := step(obj, rec, serverName); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

func readHumans(obj pyjson.Object, rec *TeamRecord, serverName string) error {
	items, err := listOf(obj["humans"], 1, humansMax)
	if err != nil {
		return err
	}
	humans, ok := stringsWhere(items, func(s string) bool { return IsHumanUserID(s, serverName) })
	if !ok {
		return distrust("humans")
	}
	rec.Humans = sortedCopy(humans)
	return nil
}

func readRoles(obj pyjson.Object, rec *TeamRecord, serverName string) error {
	roles, ok := obj["roles"].(pyjson.Object)
	if !ok || len(roles) > rolesMax {
		return distrust("roles")
	}
	rec.Roles = make(map[string]string, len(roles))
	for user, raw := range roles {
		role, isStr := raw.(string)
		role = NormaliseRole(role)
		if !isStr || !slices.Contains(knownRoles, role) || !IsAgentUserID(user, serverName) {
			return distrust("roles")
		}
		rec.Roles[user] = role
	}
	return nil
}

func readPathPrefixes(obj pyjson.Object, rec *TeamRecord, _ string) error {
	rec.PathPrefixes = []string{}
	raw, given := obj["path_prefixes"]
	if !given {
		return nil
	}
	items, err := listOf(raw, 1, pathPrefixesMax)
	if err != nil {
		return err
	}
	prefixes, ok := stringsWhere(items, IsPathPrefix)
	if !ok {
		return distrust("path_prefixes")
	}
	rec.PathPrefixes = prefixes
	return nil
}

func readForgeAPI(obj pyjson.Object, rec *TeamRecord, _ string) error {
	forge, ok := obj["forge_api"].(string)
	if !ok || !forgeAPIRE.MatchString(forge) {
		return distrust("forge_api")
	}
	rec.ForgeAPI = forge
	return nil
}

func readOrg(obj pyjson.Object, rec *TeamRecord, _ string) error {
	raw, given := obj["org"]
	if !given {
		return nil
	}
	org, ok := raw.(string)
	if !ok || !IsOwner(org) {
		return distrust("org")
	}
	rec.Org = org
	return nil
}

func readRepos(obj pyjson.Object, rec *TeamRecord, _ string) error {
	items, err := listOf(obj["repos"], 0, reposMax)
	if err != nil {
		return err
	}
	rec.Repos = make(map[string][]string, len(items))
	for _, item := range items {
		if err := readRepoEntry(item, rec.Repos); err != nil {
			return err
		}
	}
	if rec.Org == "" && len(rec.Repos) == 0 {
		return distrust("neither an org nor a repository is trusted")
	}
	return nil
}

func readRepoEntry(item any, repos map[string][]string) error {
	entry, ok := item.(pyjson.Object)
	if !ok || (!hasExactKeys(entry, "repo") && !hasExactKeys(entry, "repo", "branches")) {
		return distrust(reasonReposItem)
	}
	name, isStr := entry["repo"].(string)
	if _, seen := repos[name]; !isStr || !IsOwnerRepo(name) || seen {
		return distrust(reasonReposItem)
	}
	raw, given := entry["branches"]
	if !given {
		repos[name] = []string{}
		return nil
	}
	items, err := listOf(raw, 1, branchesMax)
	if err != nil {
		return err
	}
	branches, all := stringsWhere(items, IsBranch)
	if !all {
		return distrust("branch name")
	}
	repos[name] = branches
	return nil
}

func readExclude(obj pyjson.Object, rec *TeamRecord, _ string) error {
	rec.Exclude = []string{}
	raw, given := obj["exclude"]
	if !given {
		return nil
	}
	items, err := listOf(raw, 1, excludeMax)
	if err != nil {
		return err
	}
	names, all := stringsWhere(items, IsRepoName)
	if rec.Org == "" || !all {
		return distrust("exclude")
	}
	for _, n := range names {
		if _, listed := rec.Repos[rec.Org+"/"+n]; listed {
			return distrust("exclude repository is also listed")
		}
	}
	rec.Exclude = names
	return nil
}

// ParseTeamEvent is SPEC P44: the team record state event, sent by the admin with the empty
// state key, then P43 on its content.
func ParseTeamEvent(event any, admin, serverName, teamName string) (*TeamRecord, error) {
	content, ok := adminStateContent(event, TypeTeam, admin)
	if !ok {
		return nil, distrust("team record event")
	}
	return ParseTeamRecord(content, serverName, teamName)
}

// adminStateContent is an object event of the type with the empty state key sent by the admin,
// and its content (which may be anything).
func adminStateContent(event any, kind, admin string) (any, bool) {
	obj, ok := event.(pyjson.Object)
	if !ok {
		return nil, false
	}
	typ, isType := obj["type"].(string)
	key, isKey := obj["state_key"].(string)
	sender, isSender := obj["sender"].(string)
	if !isType || !isKey || !isSender || typ != kind || key != "" || sender != admin {
		return nil, false
	}
	return obj["content"], true
}

// ReadStatus is SPEC P45: the until (Unix ms) of a valid agent_bus.status event, and false
// for one to be ignored. An until past int64 is ignored (D11); a content with a lone surrogate
// is ignored (D10). Staleness is the caller's.
func ReadStatus(event any, ctx *Context) (int64, bool) {
	content, ok := statusContent(event, ctx)
	if !ok {
		return 0, false
	}
	if size, err := ContentSize(content); err != nil || size > maxStatusBytes {
		return 0, false
	}
	until, isNum := content["until"].(pyjson.Number)
	value, fits := until.Int64()
	if !isIntOne(content["v"]) || content["state"] != statusListening || !isNum || !fits || value < 0 {
		return 0, false
	}
	return value, true
}

// statusContent is the content of a status event a role holder set under its own state key,
// when it has exactly the status keys.
func statusContent(event any, ctx *Context) (pyjson.Object, bool) {
	obj, ok := event.(pyjson.Object)
	if !ok || obj["type"] != TypeStatus {
		return nil, false
	}
	sender, isStr := obj["sender"].(string)
	key, isKey := obj["state_key"].(string)
	if !isStr || !isKey || key != sender || !slices.Contains(knownRoles, SenderClass(sender, ctx)) {
		return nil, false
	}
	content, isObj := obj["content"].(pyjson.Object)
	return content, isObj && hasExactKeys(content, statusKeys...)
}

// ExpectedPowerLevels is SPEC P46: the exact m.room.power_levels content of a trusted team room.
func ExpectedPowerLevels(humans []string) pyjson.Object {
	return levelSpec{
		users: usersAt(humans, humanPower), notify: humanPower, reaction: humanPower,
	}.content()
}

// levelSpec is what differs between the power levels of the team room and the side rooms.
type levelSpec struct {
	users                                                 pyjson.Object
	usersDefault, eventsDefault, notify, reaction, status int64
}

func (l levelSpec) content() pyjson.Object {
	return pyjson.Object{
		"users": l.users, "users_default": intNum(l.usersDefault), "events_default": intNum(l.eventsDefault),
		"state_default": intNum(closedPower), "invite": intNum(closedPower), "kick": intNum(closedPower),
		"ban": intNum(closedPower), "redact": intNum(closedPower),
		"notifications": pyjson.Object{"room": intNum(l.notify)},
		"events": pyjson.Object{
			"m.room.power_levels": intNum(closedPower), "m.room.tombstone": intNum(tombstonePower),
			"m.room.redaction": intNum(closedPower), "m.reaction": intNum(l.reaction),
			"m.sticker": intNum(closedPower), TypeStatus: intNum(l.status),
		},
	}
}

func usersAt(humans []string, level int64) pyjson.Object {
	users := pyjson.Object{}
	for _, h := range humans {
		users[h] = intNum(level)
	}
	return users
}

// CheckPowerLevels is SPEC P46: an UntrustedError unless the content is exactly
// ExpectedPowerLevels, types included.
func CheckPowerLevels(content any, humans []string) error {
	return exactly(content, ExpectedPowerLevels(humans), "power levels")
}

func exactly(content, want any, reason string) error {
	if pyjson.Canonical(content) != pyjson.Canonical(want) {
		return distrust(reason)
	}
	return nil
}
