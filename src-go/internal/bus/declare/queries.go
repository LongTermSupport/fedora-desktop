// Package declare is the read side of helpers/agent_bus/declare.py (Plan 00209
// SPEC-view.md V1-V23): a pure reader of the host_vars text that answers what declare.py's
// subcommands declared, limited, undescribed, teams-for, choices, element-teams, address,
// settings, next-steps, github-remote and lxc-members print, as typed values, with v1's
// validation, error wording, error order and exit class. The caller reads the file (Read
// does v1's file rules); nothing here runs a process or touches the file system.
//
// v1's reader is a line reader of one YAML subset, not a YAML parser, and so is this one:
// anchors, aliases and every other YAML form are text, as v1 reads them (V9).
package declare

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// HostVars is the text of the host_vars file, as Read returns it. Each query reads it whole,
// as each declare.py run did.
type HostVars string

// settingsKeys is declare.py's SETTINGS_KEYS, in the order ccy-teams shows them.
var settingsKeys = []string{
	keyTeam, "description", "humans", "org", keyRepos, "exclude", "path_prefixes",
	"allow_from", "port", "listen", "server_name", "forge_api", "removed_humans",
}

// Declared is declare.py declared (V10): every agent_bus_teams entry, in file order.
func (h HostVars) Declared() ([]Team, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	teams := make([]Team, 0, len(hv.teams))
	for _, e := range hv.teams {
		team := Team{Name: e.team, Port: e.port, Present: e.state == statePresent, ServerName: e.serverName}
		host := e.listen
		if host == "" {
			host = hv.address
		}
		if host != "" && e.hasPort {
			team.URL = "http://" + urlHost(host) + ":" + strconv.Itoa(e.port)
		}
		if team.Description, team.DescriptionState, err = descriptionField(e); err != nil {
			return nil, err
		}
		if err := printable(team.URL); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, nil
}

func urlHost(address string) string {
	if strings.Contains(address, ":") {
		return "[" + address + "]"
	}
	return address
}

// descriptionField is declare.py's _description_field (V11).
func descriptionField(e entry) (string, DescriptionState, error) {
	found, err := rawValue(e, "description")
	switch {
	case isVaulted(err):
		return "", DescriptionVaulted, nil
	case isRefusal(err):
		return "", DescriptionInvalid, nil
	case err != nil:
		return "", "", err
	case found == pycompat.None{}:
		return "", "", nil
	}
	text, ok := found.(pycompat.Str)
	if !ok || !isDescription(string(text)) {
		return "", DescriptionInvalid, nil
	}
	return string(text), "", printable(string(text))
}

// Limited is declare.py limited (V12): each present team whose path_prefixes is truthy.
func (h HostVars) Limited() ([]Limit, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	var limits []Limit
	for _, e := range hv.teams {
		if e.state != statePresent {
			continue
		}
		prefixes, err := rawValue(e, "path_prefixes")
		if err != nil {
			return nil, err
		}
		if !pycompat.Truthy(prefixes) {
			continue
		}
		each, err := iterate(prefixes, e, "path_prefixes")
		if err != nil {
			return nil, err
		}
		words := make([]string, 0, len(each))
		for _, p := range each {
			words = append(words, pycompat.PyStr(p))
		}
		trusts, err := scopeWords(e)
		if err != nil {
			return nil, err
		}
		limit := Limit{Team: e.team, Prefixes: strings.Join(words, ", "), Trusts: trusts}
		if err := printable(limit.Prefixes, limit.Trusts); err != nil {
			return nil, err
		}
		limits = append(limits, limit)
	}
	return limits, nil
}

// repoItems is the mappings among the items of the entry's repos: `_raw_value(team, "repos")
// or []` iterated, the items that are not mappings skipped.
func repoItems(e entry) ([]*pycompat.Dict, error) {
	repos, err := rawValue(e, keyRepos)
	if err != nil || !pycompat.Truthy(repos) {
		return nil, err
	}
	each, err := iterate(repos, e, keyRepos)
	if err != nil {
		return nil, err
	}
	var out []*pycompat.Dict
	for _, r := range each {
		if d, ok := r.(*pycompat.Dict); ok {
			out = append(out, d)
		}
	}
	return out, nil
}

// iterate is python's iter() over the entry's value: a value v1 could not iterate is the
// TypeError it crashed on (B-S3).
func iterate(v pycompat.Value, e entry, key string) ([]pycompat.Value, error) {
	each, err := pycompat.Iterate(v)
	if err != nil {
		return nil, crashf("%s's %s: %v", e.team, key, err)
	}
	return each, nil
}

// scopeWords is declare.py's scope_words (V14): what the team trusts, as a person says it.
func scopeWords(e entry) (string, error) {
	org, err := rawValue(e, "org")
	if err != nil {
		return "", err
	}
	repos, err := repoItems(e)
	if err != nil {
		return "", err
	}
	var parts []string
	if pycompat.Truthy(org) {
		parts = append(parts, "any repository of "+pycompat.PyStr(org))
	}
	for _, r := range repos {
		repo, has := r.Get("repo")
		if !has {
			continue
		}
		s, ok := repo.(pycompat.Str)
		if !ok {
			return "", crashf("%s's repos: a repo is %s, not a string", e.team, repo.TypeName())
		}
		parts = append(parts, string(s))
	}
	if len(parts) == 0 {
		return "no repository", nil
	}
	return strings.Join(parts, " or "), nil
}

// scopeOrUnreadable is declare.py's _scope_or_unreadable: a refusal reads as words.
func scopeOrUnreadable(e entry) (string, error) {
	words, err := scopeWords(e)
	if isRefusal(err) {
		return "a scope ccy-teams cannot read", nil
	}
	return words, err
}

// Undescribed is declare.py undescribed (V13): each present team with no description key.
func (h HostVars) Undescribed() ([]Undescribed, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	var out []Undescribed
	for _, e := range hv.teams {
		if _, has := find(e.raw, "description"); e.state != statePresent || has {
			continue
		}
		trusts, err := scopeOrUnreadable(e)
		if err != nil {
			return nil, err
		}
		if err := printable(trusts); err != nil {
			return nil, err
		}
		out = append(out, Undescribed{Team: e.team, Trusts: trusts})
	}
	return out, nil
}

// checkedRepo is declare.py's _checked_repo (V15): the owner and name of a lower-case
// OWNER/REPO. Its second refusal (parse_ref of "issue:R#1") cannot follow the first's pass:
// every OWNER/REPO fits the issue reference and its length limit.
func checkedRepo(repo string) (string, string, error) {
	if !isOwnerRepo(repo) {
		return "", "", usagef("--repo: %s is not a lower-case OWNER/REPO", pycompat.PyRepr(pycompat.Str(repo)))
	}
	slash := strings.IndexByte(repo, '/')
	return repo[:slash], repo[slash+1:], nil
}

// standing is declare.py's _standing (V16).
func standing(e entry, owner, name string) (Standing, error) {
	repos, err := repoItems(e)
	if err != nil {
		return "", err
	}
	full := owner + "/" + name
	for _, r := range repos {
		if repo, has := r.Get("repo"); has && pycompat.Equal(repo, pycompat.Str(full)) {
			return StandingCovers, nil
		}
	}
	org, err := rawValue(e, "org")
	if err != nil {
		return "", err
	}
	if !pycompat.Equal(org, pycompat.Str(owner)) {
		return StandingWiden, nil
	}
	exclude, err := rawValue(e, "exclude")
	if err != nil || !pycompat.Truthy(exclude) {
		return StandingCovers, err
	}
	in, err := pycompat.Contains(exclude, pycompat.Str(name))
	if err != nil {
		return "", crashf("%s's exclude: %v", e.team, err)
	}
	if in {
		return StandingExcluded, nil
	}
	return StandingCovers, nil
}

// TeamsFor is declare.py teams-for (V15): the present teams whose standing to the
// repository is covers. The file is read before the repository is judged, as v1 does.
func (h HostVars) TeamsFor(repo string) ([]string, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	owner, name, err := checkedRepo(repo)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range hv.teams {
		if e.state != statePresent {
			continue
		}
		s, err := standing(e, owner, name)
		if err != nil {
			return nil, err
		}
		if s == StandingCovers {
			names = append(names, e.team)
		}
	}
	return names, nil
}

// Choices is declare.py choices (V17): every present team, its standing (unreadable when a
// value it needs is refused) and its description field.
func (h HostVars) Choices(repo string) ([]Choice, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	owner, name, err := checkedRepo(repo)
	if err != nil {
		return nil, err
	}
	var out []Choice
	for _, e := range hv.teams {
		if e.state != statePresent {
			continue
		}
		s, err := standing(e, owner, name)
		if isRefusal(err) {
			s, err = StandingUnreadable, nil
		}
		if err != nil {
			return nil, err
		}
		choice := Choice{Team: e.team, Standing: s}
		if choice.Description, choice.DescriptionState, err = descriptionField(e); err != nil {
			return nil, err
		}
		out = append(out, choice)
	}
	return out, nil
}

// ElementTeams is declare.py element-teams (V18): each agent_bus_element_teams entry and
// python's str() of its base_url ("None" when it has none).
func (h HostVars) ElementTeams() ([]Profile, error) {
	hv, err := parse(string(h), elementKey)
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(hv.teams))
	for _, e := range hv.teams {
		url, err := rawValue(e, "base_url")
		if err != nil {
			return nil, err
		}
		profile := Profile{Team: e.team, BaseURL: pycompat.PyStr(url)}
		if err := printable(profile.BaseURL); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// Address is declare.py address (V19): the bus address, or "". The whole teams list is read,
// as v1 reads it.
func (h HostVars) Address() (string, error) {
	hv, err := parse(string(h), teamsKey)
	return hv.address, err
}

// present is declare.py's _present: the first team of that name declared present, which must
// have a port.
func present(hv hostVars, name string) (entry, error) {
	for _, e := range hv.teams {
		if e.team == name && e.state == statePresent && e.hasPort {
			return e, nil
		}
		if e.team == name && e.state == statePresent {
			break
		}
	}
	return entry{}, usagef("--team: team %s is not declared present, with a port, in %s", name, teamsKey)
}

// Settings is declare.py settings (V20): each key of SETTINGS_KEYS of a present team, its
// value as json.dumps(ensure_ascii=False) writes it, server_name with ensure_ascii.
func (h HostVars) Settings(team string) ([]Setting, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return nil, err
	}
	e, err := present(hv, team)
	if err != nil {
		return nil, err
	}
	out := make([]Setting, 0, len(settingsKeys))
	for _, key := range settingsKeys {
		s, err := setting(e, key)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func setting(e entry, key string) (Setting, error) {
	if key == "server_name" {
		text, err := pycompat.Dumps(pycompat.Str(e.serverName), true)
		return Setting{Key: key, Value: json.RawMessage(text)}, err
	}
	v, err := rawValue(e, key)
	switch {
	case isVaulted(err):
		return Setting{Key: key, State: SettingVaulted}, nil
	case isRefusal(err):
		return Setting{Key: key, State: SettingInvalid}, nil
	case err != nil:
		return Setting{}, err
	case v == pycompat.None{}:
		return Setting{Key: key}, nil
	}
	text, err := pycompat.Dumps(v, false)
	if err != nil {
		return Setting{}, crashf("%s's %s: %v", e.team, key, err)
	}
	return Setting{Key: key, Value: json.RawMessage(text)}, nil
}

// NextSteps is declare.py next-steps (V21): what is left once the team is installed.
func (h HostVars) NextSteps(team string) (string, error) {
	hv, err := parse(string(h), teamsKey)
	if err != nil {
		return "", err
	}
	e, err := present(hv, team)
	if err != nil {
		return "", err
	}
	scope, err := scopeWords(e)
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf("Team %s is up. Later, in ccy-teams:\n", team) +
		"  Sign in      Enter on your name under the team, then choose Sign in: it sets a password and shows it.\n" +
		"  Element      Enter on the team, then choose Open in Element.\n" +
		fmt.Sprintf("  Agents       in a checkout of %s, run ccy --teams %s\n", scope, team) +
		"               (ccy-teams run there offers it; Enter on the team, then Start a ccy session, does it too).\n"
	return text, printable(text)
}
