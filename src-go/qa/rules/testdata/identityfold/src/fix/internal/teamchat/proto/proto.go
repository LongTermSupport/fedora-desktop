// Package proto stands for teamchat's proto: the lower-case GitHub identity grammars (an owner,
// an OWNER/REPO, a reference, a forge section), the input folds that canonicalise a name before
// them, and the types whose fields carry a name. Its own calls of its grammars are not judged.
package proto

import "strings"

// Value stands for a parsed JSON value.
type Value struct{ Text string }

// Forge is a forge section.
type Forge struct {
	Kind    string
	Org     string
	Repos   []ForgeRepo
	Exclude []string
}

// ForgeRepo is one listed repository.
type ForgeRepo struct {
	Repo     string
	Branches []string
}

// Ping is a ping's content.
type Ping struct {
	Verb   string
	Ref    string
	Branch string
}

// Ref is a parsed reference.
type Ref struct{ Owner, Repo string }

// ValidOwner is the lower-case owner grammar.
func ValidOwner(s string) bool { return s != "" && s == strings.ToLower(s) }

// ValidOwnerRepo is the lower-case OWNER/REPO grammar.
func ValidOwnerRepo(s string) bool { return strings.Contains(s, "/") && ValidOwner(s) }

// ParseRef parses a reference in its canonical, lower-case form.
func ParseRef(s string) (Ref, bool) { return Ref{}, ValidOwnerRepo(s) }

// DecodeForge decodes a forge section in its canonical form.
func DecodeForge(v Value) (Forge, error) {
	f := Forge{Org: v.Text}
	if !ValidOwner(f.Org) {
		return Forge{}, nil
	}
	return f, nil
}

// DecodeForgeInput decodes a forge section a person wrote, its names folded first.
func DecodeForgeInput(v Value) (Forge, error) {
	return DecodeForge(Value{Text: strings.ToLower(v.Text)})
}

// LowercaseRefRepo folds a reference's OWNER/REPO.
func LowercaseRefRepo(s string) string { return strings.ToLower(s) }

// Inside is proto's own use of its grammars and fields, which the rule does not judge.
func Inside(s string) Ping {
	_ = ValidOwner(s)
	return Ping{Ref: s}
}
