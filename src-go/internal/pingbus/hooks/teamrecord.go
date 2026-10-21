package hooks

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// protocol.parse_team_record's bounds and patterns (S22).
const (
	humansMax       = 16
	rolesMax        = 64
	reposMax        = 32
	branchesMax     = 8
	pathPrefixesMax = 32
	pathMaxSegments = 8
	forgePrefix     = "https://"
)

var (
	teamKeys         = []string{"v", keyTeam, "humans", keyRoles, "org", "exclude", "repos", "path_prefixes", "forge_api"}
	teamOptionalKeys = []string{"org", "exclude", "path_prefixes"}
	ownerPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)
	ownerRepoPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?/([a-z0-9._-]{1,100})$`)
	repoNamePattern  = regexp.MustCompile(`^[a-z0-9._-]{1,100}$`)
	branchPattern    = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	segmentPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// untrusted is protocol.Untrusted: a team record that is not exactly what spec section 8 allows.
func untrusted(why string) error { return untrustedErr("%s", why) }

// teamRecord is protocol.TeamRecord, and its Context for the validator.
type teamRecord struct {
	team         string
	humans       map[string]bool
	roles        map[string]string
	repos        map[string][]string
	pathPrefixes []string
	forgeAPI     string
	org          string
	hasOrg       bool
	exclude      []string
}

// parseTeamRecord is protocol.parse_team_record (S22), in v1's order of checks and with its
// reasons, the legacy orchestrator read as coordinator.
func parseTeamRecord(content any, server, team string) (teamRecord, error) {
	record, ok := content.(pyjson.Object)
	if !ok || !hasTeamKeys(record) {
		return teamRecord{}, untrusted("team record keys")
	}
	if v, isNumber := record["v"].(pyjson.Number); !isNumber || !v.IsInt(1) {
		return teamRecord{}, untrusted("team record version")
	}
	if name, isText := record[keyTeam].(string); !isText || !teamNamePattern.MatchString(name) || name != team {
		return teamRecord{}, untrusted("team name")
	}
	got := teamRecord{team: team}
	humans, err := parseHumans(record["humans"], server)
	if err != nil {
		return teamRecord{}, err
	}
	got.humans = humans
	roles, err := parseRoles(record[keyRoles], server)
	if err != nil {
		return teamRecord{}, err
	}
	got.roles = roles
	if got.pathPrefixes, err = parsePathPrefixes(record); err != nil {
		return teamRecord{}, err
	}
	forge, isText := record["forge_api"].(string)
	if !isText || !isForgeAPI(forge) {
		return teamRecord{}, untrusted("forge_api")
	}
	got.forgeAPI = forge
	if err := got.readTrust(record); err != nil {
		return teamRecord{}, err
	}
	return got, nil
}

// parseHumans is the record's humans: 1 to humansMax distinct human user IDs of this server.
func parseHumans(value any, server string) (map[string]bool, error) {
	humans, ok := listOf(value, 1, humansMax)
	if !ok {
		return nil, untrusted(listRefusal)
	}
	if !all(humans, func(h any) bool { return isHumanUser(h, server) }) {
		return nil, untrusted("humans")
	}
	set := make(map[string]bool, len(humans))
	for _, h := range texts(humans) {
		set[h] = true
	}
	return set, nil
}

// listRefusal is protocol._list_of's reason.
const listRefusal = "list size or duplicates"

// parsePathPrefixes is the optional path_prefixes: a list of path prefixes, none when absent.
func parsePathPrefixes(record pyjson.Object) ([]string, error) {
	value, present := record["path_prefixes"]
	if !present {
		return nil, nil
	}
	prefixes, isList := listOf(value, 1, pathPrefixesMax)
	if !isList {
		return nil, untrusted(listRefusal)
	}
	if !all(prefixes, isPathPrefix) {
		return nil, untrusted("path_prefixes")
	}
	return texts(prefixes), nil
}

// texts is a list already checked to hold only strings.
func texts(items []any) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = text(item)
	}
	return names
}

func hasTeamKeys(record pyjson.Object) bool {
	for key := range record {
		if !slices.Contains(teamKeys, key) {
			return false
		}
	}
	for _, key := range teamKeys {
		if _, present := record[key]; !present && !slices.Contains(teamOptionalKeys, key) {
			return false
		}
	}
	return true
}

// parseRoles is the record's roles: at most rolesMax agents of this server, each coordinator
// or worker once the legacy name is read.
func parseRoles(value any, server string) (map[string]string, error) {
	object, ok := value.(pyjson.Object)
	if !ok || len(object) > rolesMax {
		return nil, untrusted(keyRoles)
	}
	roles := make(map[string]string, len(object))
	for user, role := range object {
		name, isText := role.(string)
		if name == roleLegacyLeader {
			name = roleCoordinator
		}
		if _, _, agent := agentUser(user, server); !isText || (name != roleCoordinator && name != roleWorker) || !agent {
			return nil, untrusted(keyRoles)
		}
		roles[user] = name
	}
	return roles, nil
}

// readTrust is the record's org, repos and exclude: an org or at least one repository, and
// an exclude only of the org's repositories that repos does not list.
func (r *teamRecord) readTrust(record pyjson.Object) error {
	if value, present := record["org"]; present {
		name, isText := value.(string)
		if !isText || !ownerPattern.MatchString(name) {
			return untrusted("org")
		}
		r.org, r.hasOrg = name, true
	}
	repos, err := parseRepos(record["repos"])
	if err != nil {
		return err
	}
	r.repos = repos
	if !r.hasOrg && len(repos) == 0 {
		return untrusted("neither an org nor a repository is trusted")
	}
	value, present := record["exclude"]
	if !present {
		return nil
	}
	names, ok := listOf(value, 1, reposMax)
	if !ok {
		return untrusted(listRefusal)
	}
	if !r.hasOrg || !all(names, isRepoName) {
		return untrusted("exclude")
	}
	for _, name := range texts(names) {
		if _, listed := repos[r.org+"/"+name]; listed {
			return untrusted("exclude repository is also listed")
		}
	}
	r.exclude = texts(names)
	return nil
}

// parseRepos is protocol._parse_repos: each OWNER/REPO once, with its branches, an empty
// list (not nil) for any branch.
func parseRepos(value any) (map[string][]string, error) {
	items, ok := listOf(value, 0, reposMax)
	if !ok {
		return nil, untrusted(listRefusal)
	}
	repos := make(map[string][]string, len(items))
	for _, item := range items {
		entry, isObject := item.(pyjson.Object)
		_, hasRepo := entry["repo"]
		_, hasBranches := entry["branches"]
		if !isObject || !hasRepo || len(entry) != 1+boolInt(hasBranches) {
			return nil, untrusted("repos entry")
		}
		name, isText := entry["repo"].(string)
		if _, twice := repos[name]; !isText || !isOwnerRepo(name) || twice {
			return nil, untrusted("repos entry")
		}
		if !hasBranches {
			repos[name] = []string{}
			continue
		}
		branches, isList := listOf(entry["branches"], 1, branchesMax)
		if !isList {
			return nil, untrusted(listRefusal)
		}
		if !all(branches, func(b any) bool { return matches(branchPattern, b) }) {
			return nil, untrusted("branch name")
		}
		repos[name] = texts(branches)
	}
	return repos, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// listOf is protocol._list_of: a list of low to high entries, no two the same as
// json.dumps(sort_keys=True) writes them.
func listOf(value any, low, high int) ([]any, bool) {
	list, ok := value.([]any)
	if !ok || len(list) < low || len(list) > high {
		return nil, false
	}
	seen := make(map[string]bool, len(list))
	for _, item := range list {
		key := pyjson.Canonical(item)
		if seen[key] {
			return nil, false
		}
		seen[key] = true
	}
	return list, true
}

func all(items []any, ok func(any) bool) bool {
	return !slices.ContainsFunc(items, func(item any) bool { return !ok(item) })
}

func matches(pattern *regexp.Regexp, value any) bool {
	text, ok := value.(string)
	return ok && pattern.MatchString(text)
}

func isRepoNameText(name string) bool {
	return name != "." && name != ".." && !strings.HasSuffix(name, ".git")
}

// isOwnerRepo is protocol.is_owner_repo.
func isOwnerRepo(value string) bool {
	m := ownerRepoPattern.FindStringSubmatch(value)
	return m != nil && isRepoNameText(m[1])
}

// isRepoName is protocol.is_repo_name.
func isRepoName(value any) bool {
	text, ok := value.(string)
	return ok && repoNamePattern.MatchString(text) && isRepoNameText(text)
}

// isPathPrefix is protocol.is_path_prefix: a PATH ending in "/", or an exact file PATH, of at
// most pathMaxSegments segments, none "." or "..".
func isPathPrefix(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	segments := strings.Split(strings.TrimSuffix(text, "/"), "/")
	if len(segments) > pathMaxSegments {
		return false
	}
	for _, segment := range segments {
		if !segmentPattern.MatchString(segment) || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// isForgeAPI is protocol's FORGE_API_PATTERN, `https://[^\s/]+(/\S*)?`, with Python's \s: the
// characters str.isspace() calls whitespace, which are Go's unicode.IsSpace and U+001C to
// U+001F.
func isForgeAPI(value string) bool {
	rest, ok := strings.CutPrefix(value, forgePrefix)
	if !ok {
		return false
	}
	host := strings.IndexFunc(rest, func(r rune) bool { return r == '/' || isPySpace(r) })
	switch {
	case host == 0:
		return false
	case host < 0:
		return rest != ""
	case rest[host] != '/':
		return false
	}
	return strings.IndexFunc(rest[host:], isPySpace) < 0
}

func isPySpace(r rune) bool {
	return r != utf8.RuneError && (unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f))
}
