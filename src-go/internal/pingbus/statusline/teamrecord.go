package statusline

import (
	"errors"
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
	teamKeys         = []string{"v", "team", "humans", "roles", "org", "exclude", "repos", "path_prefixes", "forge_api"}
	teamOptionalKeys = []string{"org", "exclude", "path_prefixes"}
	ownerPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)
	ownerRepoPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?/([a-z0-9._-]{1,100})$`)
	repoNamePattern  = regexp.MustCompile(`^[a-z0-9._-]{1,100}$`)
	branchPattern    = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	segmentPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// untrusted is protocol.Untrusted: a team record that is not exactly what spec section 8 allows.
func untrusted(why string) error { return errors.New("team record: " + why) }

// parseTeamRecord is protocol.parse_team_record: the record's roles, user ID to role, with the
// legacy orchestrator read as coordinator (S22).
func parseTeamRecord(content any, server, team string) (map[string]string, error) {
	record, ok := content.(pyjson.Object)
	if !ok || !hasTeamKeys(record) {
		return nil, untrusted("keys")
	}
	if v, isNumber := record["v"].(pyjson.Number); !isNumber || !v.IsInt(1) {
		return nil, untrusted("version")
	}
	if name, isText := record["team"].(string); !isText || !teamNamePattern.MatchString(name) || name != team {
		return nil, untrusted("team name")
	}
	humans, ok := listOf(record["humans"], 1, humansMax)
	if !ok || !all(humans, func(h any) bool { return isHumanUser(h, server) }) {
		return nil, untrusted("humans")
	}
	roles, err := parseRoles(record["roles"], server)
	if err != nil {
		return nil, err
	}
	if value, present := record["path_prefixes"]; present {
		prefixes, isList := listOf(value, 1, pathPrefixesMax)
		if !isList || !all(prefixes, isPathPrefix) {
			return nil, untrusted("path_prefixes")
		}
	}
	if forge, isText := record["forge_api"].(string); !isText || !isForgeAPI(forge) {
		return nil, untrusted("forge_api")
	}
	return roles, checkTrust(record)
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
		return nil, untrusted("roles")
	}
	roles := make(map[string]string, len(object))
	for user, role := range object {
		name, isText := role.(string)
		if name == roleLegacyLeader {
			name = roleCoordinator
		}
		if _, _, agent := agentUser(user, server); !isText || (name != roleCoordinator && name != roleWorker) || !agent {
			return nil, untrusted("roles")
		}
		roles[user] = name
	}
	return roles, nil
}

// checkTrust is the record's org, repos and exclude: an org or at least one repository, and
// an exclude only of the org's repositories that repos does not list.
func checkTrust(record pyjson.Object) error {
	org, hasOrg := "", false
	if value, present := record["org"]; present {
		text, isText := value.(string)
		if !isText || !ownerPattern.MatchString(text) {
			return untrusted("org")
		}
		org, hasOrg = text, true
	}
	repos, err := parseRepos(record["repos"])
	if err != nil {
		return err
	}
	if !hasOrg && len(repos) == 0 {
		return untrusted("neither an org nor a repository is trusted")
	}
	value, present := record["exclude"]
	if !present {
		return nil
	}
	names, ok := listOf(value, 1, reposMax)
	if !ok || !hasOrg || !all(names, isRepoName) {
		return untrusted("exclude")
	}
	for _, name := range names {
		if text, isText := name.(string); isText && slices.Contains(repos, org+"/"+text) {
			return untrusted("exclude repository is also listed")
		}
	}
	return nil
}

// parseRepos is protocol._parse_repos: the listed OWNER/REPO names, each once, with optional
// branches.
func parseRepos(value any) ([]string, error) {
	items, ok := listOf(value, 0, reposMax)
	if !ok {
		return nil, untrusted("list size or duplicates")
	}
	var names []string
	for _, item := range items {
		entry, isObject := item.(pyjson.Object)
		_, hasRepo := entry["repo"]
		_, hasBranches := entry["branches"]
		if !isObject || !hasRepo || len(entry) != 1+boolInt(hasBranches) {
			return nil, untrusted("repos entry")
		}
		name, isText := entry["repo"].(string)
		if !isText || !isOwnerRepo(name) || slices.Contains(names, name) {
			return nil, untrusted("repos entry")
		}
		names = append(names, name)
		if !hasBranches {
			continue
		}
		branches, isList := listOf(entry["branches"], 1, branchesMax)
		if !isList || !all(branches, func(b any) bool { return matches(branchPattern, b) }) {
			return nil, untrusted("branch name")
		}
	}
	return names, nil
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
	if strings.HasSuffix(text, "/") {
		text = text[:len(text)-1]
	}
	segments := strings.Split(text, "/")
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
