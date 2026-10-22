package declare

import "encoding/json"

// The answers, with the names, fields and meanings internal/declare's types have, so that it
// can alias them and its callers do not change.

// Team is a TEAM line of "declare declared": one agent_bus_teams entry.
type Team struct {
	Name       string
	Port       int // 0 when the entry sets none
	Present    bool
	ServerName string
	URL        string // empty when the entry has no address to reach it on
	// Description is what the team is for; empty when the entry sets none (or a vaulted one).
	Description string
	// DescriptionState is set when the entry has a description that cannot be shown: it is
	// vaulted, or fails the team file's grammar. Description is then empty.
	DescriptionState DescriptionState
}

// DescriptionState says why a team's description is not shown.
type DescriptionState string

// The two states declare.py marks with !vaulted and !invalid.
const (
	DescriptionVaulted DescriptionState = "vaulted"
	DescriptionInvalid DescriptionState = "invalid"
)

// Limit is a LIMITED line: a team still limited to some folders.
type Limit struct {
	Team     string
	Prefixes string // "docs/, CLAUDE/Plan/"
	Trusts   string // what the team trusts, in words
}

// Undescribed is an UNDESCRIBED line: a present team that has no description yet.
type Undescribed struct {
	Team   string
	Trusts string // what the team trusts, in words
}

// Profile is an ELEMENT line: a team's Element profile and the homeserver it uses.
type Profile struct {
	Team    string
	BaseURL string
}

// Standing is how a present team stands to a repository, the word declare.py choices prints.
type Standing string

// The standings of declare.py choices.
const (
	// StandingCovers: the team trusts the repository now.
	StandingCovers Standing = "covers"
	// StandingWiden: it does not, and adding the repository to its scope would make it.
	StandingWiden Standing = "widen"
	// StandingExcluded: the team's org owns it and excludes it on purpose.
	StandingExcluded Standing = "excluded"
	// StandingUnreadable: the team's scope cannot be read (vaulted), so nothing can be said.
	StandingUnreadable Standing = "unreadable"
)

// Choice is a CHOICE line: a present team, how it stands to the repository, what it is for.
type Choice struct {
	Team             string
	Standing         Standing
	Description      string
	DescriptionState DescriptionState
}

// Setting is a SETTING line of "declare settings": one key of a present team's entry.
type Setting struct {
	Key string
	// Value is the key's value as declare.py's json.dumps writes it; nil when the entry does
	// not set it or it cannot be read.
	Value json.RawMessage
	// State is set when the entry has a value that cannot be shown.
	State SettingState
}

// SettingState says why a setting's value is not shown.
type SettingState string

// The two states declare.py marks with !vaulted and !invalid.
const (
	SettingVaulted SettingState = "vaulted"
	SettingInvalid SettingState = "invalid"
)

// LxcMember is an LXCMEMBER line of "declare lxc-members": an entry as the play reads it.
type LxcMember struct {
	Team      string
	Container string
	User      string
	Host      string
	Role      string // worker or coordinator (the legacy orchestrator is read as coordinator)
	State     string // present or absent
	// Checkout and Seat are the entry's own, "" where it leaves them out: an absent entry may
	// name only its repo, and a seat is usually derived.
	Checkout string
	Seat     string
}
