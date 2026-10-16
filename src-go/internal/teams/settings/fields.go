package settings

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

// field is one key of a team's entry as the settings screen shows it.
type field struct {
	key, label string
	// list is a field typed as a comma-separated list.
	list bool
	// about says what the field is; applies, what applying a change to it does.
	about, applies string
	// fixed, when set, is why the field cannot be changed here.
	fixed string
}

const (
	appliesRecord = "Applying rewrites the team record in the team room, which running sessions read on their " +
		"next sync; no homeserver restart."
	fixedAddress = "every member's credentials name the homeserver's address and port, so a new one means new " +
		"credentials for every member, which ccy-teams does not do."
	commas = " Separate several with commas."
)

// fields are every key declare.py settings prints, in its order. Only the keys declare.py set
// changes are editable; the rest say why not.
var fields = []field{
	{key: "team", label: "Name", fixed: "a team's name is its homeserver's unit and data folder, part of every " +
		"account and room ID, and in every session's seats; the installer refuses to change it, so a new name " +
		"means a new team (Plan 00201 Task 2.16)."},
	{
		key: "description", label: "Description",
		about:   "What the team is for, shown wherever teams are listed or chosen: 1 to 120 characters on one line.",
		applies: "Applying sets the team room's topic, which agents read as the team's description; no homeserver restart.",
	},
	{
		key: "humans", label: "People", list: true,
		about: "The people of the team, by Element user name; 1 to 16. A name removed earlier can never be " +
			"added back." + commas,
		applies: "Applying makes an account for each person added and invites them (each then signs in: Enter on " +
			"their name, then Sign in); a person removed is put out of the room and their account deactivated " +
			"for good: that name can never be added back. No homeserver restart.",
	},
	{
		key: "org", label: "GitHub owner",
		about: "The GitHub owner (an organisation or a user) whose every repository the team trusts. Empty for " +
			"none; the team then needs a repository listed.",
		applies: appliesRecord,
	},
	{
		key: "repos", label: "Repositories", list: true,
		about:   "Repositories the team trusts besides its owner's, as OWNER/REPO; branches already listed for one are kept." + commas,
		applies: appliesRecord,
	},
	{
		key: "exclude", label: "Excluded repositories", list: true,
		about:   "Repositories of the GitHub owner the team never trusts, by name without the owner." + commas,
		applies: appliesRecord,
	},
	{
		key: "path_prefixes", label: "Path limit", list: true,
		about:   "The only folders a trusted reference may point at; empty for any path." + commas,
		applies: appliesRecord,
	},
	{
		key: "allow_from", label: "Allowed networks", list: true,
		about: "Networks besides this machine that may reach the homeserver, as CIDR (192.0.2.0/24)." + commas,
		applies: "Applying rewrites the firewall rules and the homeserver's address filter, which restarts the " +
			"homeserver: the bus is down for a few seconds.",
	},
	{key: "port", label: "Port", fixed: fixedAddress},
	{key: "listen", label: "Listen addresses", fixed: fixedAddress},
	{key: "server_name", label: "Server name", fixed: "it is part of every account and room ID, and the " +
		"homeserver cannot change it without wiping its data."},
	{key: "forge_api", label: "Forge API", fixed: "members check references only against GitHub's API " +
		"(Plan 00161 D66), so it has one allowed value."},
	{key: "removed_humans", label: "Removed people", fixed: "each was taken off the team and their account " +
		"deactivated for good, so the name can never be added back; removing a person from People adds them here."},
}

// fieldOf is the field of key; the zero field when there is none.
func fieldOf(key string) field {
	for _, f := range fields {
		if f.key == key {
			return f
		}
	}
	return field{}
}

const (
	none       = "(none)"
	unreadable = "(not readable)"
)

// shown is a setting's value as the screen prints it.
func shown(s declare.Setting) string {
	switch s.State {
	case declare.SettingVaulted:
		return "(vault-encrypted)"
	case declare.SettingInvalid:
		return unreadable
	}
	return shownJSON(s.Value)
}

// shownJSON is a JSON value as the screen prints it: a list joined with commas, a repository
// with its branches.
func shownJSON(raw json.RawMessage) string {
	if raw == nil {
		return none
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return unreadable
	}
	items, isList := value.([]any)
	if !isList {
		return scalar(value)
	}
	if len(items) == 0 {
		return none
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = scalar(item)
		if repo, ok := item.(map[string]any); ok {
			parts[i] = scalar(repo["repo"])
			if branches, ok := repo["branches"].([]any); ok && len(branches) > 0 {
				names := make([]string, len(branches))
				for j, b := range branches {
					names[j] = scalar(b)
				}
				parts[i] += " (branches " + strings.Join(names, ", ") + ")"
			}
		}
	}
	return strings.Join(parts, ", ")
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return unreadable
	}
	return string(raw)
}

// editText is a value as the input starts from: a list's items (a repository's name) joined
// with commas.
func editText(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	items, isList := value.([]any)
	if !isList {
		return scalar(value)
	}
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = scalar(item)
		if repo, ok := item.(map[string]any); ok {
			names[i] = scalar(repo["repo"])
		}
	}
	return strings.Join(names, ", ")
}

// inputJSON is what was typed as the JSON declare.py set takes: a list's items split on
// commas, trimmed, empty ones dropped.
func inputJSON(f field, text string) string {
	var value any = text
	if f.list {
		items := []string{}
		for part := range strings.SplitSeq(text, ",") {
			if item := strings.TrimSpace(part); item != "" {
				items = append(items, item)
			}
		}
		value = items
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}
