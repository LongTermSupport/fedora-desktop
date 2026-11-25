package proto

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// A reference refusal carries its cause as its Detail, so a sender's message can say what is
// wrong with the reference and what it must be (fedora-desktop#421): `done` with no reference
// was "refused (ref)" and nothing more.
func TestARefRefusalCarriesItsCause(t *testing.T) {
	excludedRef := "commit:example-org/private-notes@" + testSHA
	pathRef := "path:example-org/myrepo@" + testSHA + ":docs/a.md"
	cases := map[string]struct {
		self, answers string
		p             Ping
		prefixes      []string
		reason, cause string
	}{
		"done with no ref":      {workerID, pingEvent, Ping{Verb: VerbDone, To: []string{coordID}}, nil, ReasonRef, RefCauseMissing},
		"ack with a ref":        {workerID, pingEvent, Ping{Verb: VerbAck, To: []string{coordID}, Ref: commitRef}, nil, ReasonRef, RefCauseForbidden},
		"run-qa with a path":    {workerID, "", Ping{Verb: "run-qa", To: []string{coordID}, Ref: pathRef}, nil, ReasonRef, RefCauseForm},
		"done with no grammar":  {workerID, pingEvent, Ping{Verb: VerbDone, To: []string{coordID}, Ref: "the commit"}, nil, ReasonRef, RefCauseForm},
		"a bad branch":          {workerID, "", Ping{Verb: "review", To: []string{coordID}, Ref: commitRef, Branch: "a b"}, nil, ReasonRef, RefCauseBranch},
		"a branch with no ref":  {workerID, pingEvent, Ping{Verb: VerbNack, To: []string{coordID}, Branch: "main"}, nil, ReasonRef, RefCauseBranch},
		"sync of an external":   {coordID, "", Ping{Verb: "sync", To: []string{workerID}, Ref: externalRef}, nil, ReasonAllowlist, RefCauseNotListed},
		"review of excluded":    {workerID, "", Ping{Verb: "review", To: []string{coordID}, Ref: excludedRef}, nil, ReasonExcluded, RefCauseExcluded},
		"a path outside prefix": {workerID, "", Ping{Verb: "review", To: []string{coordID}, Ref: pathRef}, []string{"src/"}, ReasonAllowlist, RefCausePathPrefix},
	}
	for name, tc := range cases {
		c := composer(t, tc.self)
		c.Roster.Forge.PathPrefixes = tc.prefixes
		_, err := c.Ping(tc.p, tc.answers, "x")
		re, ok := errors.AsType[*RefusalError](err)
		if !ok || re.Reason != tc.reason || re.Detail != tc.cause {
			t.Errorf("%s: %v, want %s: %s", name, err, tc.reason, tc.cause)
		}
		if got := c.Roster.RefCause(tc.p); got != tc.cause {
			t.Errorf("%s: RefCause = %q, want %q", name, got, tc.cause)
		}
	}
	if got := testRoster(t).RefCause(Ping{Verb: "review", Ref: commitRef}); got != "" {
		t.Errorf("a passing reference has the cause %q", got)
	}
	if got := testRoster(t).RefCause(Ping{Verb: "nope"}); got != "" {
		t.Errorf("an unknown verb has the reference cause %q", got)
	}
}

// RefForms are the forms a verb's reference may take, in A 9.1's order, and each form's
// spelling is one ParseRef reads as that form once its placeholders are filled.
func TestRefFormsAndTheirSpellings(t *testing.T) {
	for verb, want := range map[string][]string{
		"run-qa":  {RefCommit, RefPR},
		"fetch":   {RefPath, RefCommit},
		VerbDone:  {RefPath, RefCommit, RefPR, RefIssue},
		VerbAck:   nil,
		VerbCheck: nil,
	} {
		if got := Verbs[verb].RefForms(); !slices.Equal(got, want) {
			t.Errorf("%s: RefForms = %v, want %v", verb, got, want)
		}
	}
	fill := strings.NewReplacer("OWNER", "example-org", "REPO", "myrepo", "SHA", testSHA, "PATH", "docs/a.md", "N", "7")
	for _, form := range RefForms {
		spelled, ok := RefSpellings[form]
		if !ok {
			t.Fatalf("no spelling of %s", form)
		}
		if r, parsed := ParseRef(fill.Replace(spelled)); !parsed || r.Form != form {
			t.Errorf("%s: %q does not parse as its form", form, spelled)
		}
	}
	if len(RefSpellings) != len(RefForms) {
		t.Errorf("spellings %v for the forms %v", RefSpellings, RefForms)
	}
}
