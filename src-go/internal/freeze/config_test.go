package freeze

import (
	"strings"
	"testing"
)

func TestValidateAcceptsACompleteDeclaration(t *testing.T) {
	if err := testConfig(vocabs[0]).Validate(); err != nil {
		t.Fatalf("a complete declaration loads: %v", err)
	}
}

// Each setting is checked by name, so no case here can pass against a Validate that refuses
// everything for one reason.
func TestValidateRefusesEachEmptyRequiredSettingAndNamesIt(t *testing.T) {
	cases := map[string]func(*Config){
		"Tool":         func(c *Config) { c.Tool = "" },
		"StateRunning": func(c *Config) { c.StateRunning = "" },
		"StateFrozen":  func(c *Config) { c.StateFrozen = "" },
		"HostOnlyNote": func(c *Config) { c.HostOnlyNote = "" },
		"TargetHint":   func(c *Config) { c.TargetHint = "" },
	}
	for field, blank := range cases {
		t.Run(field, func(t *testing.T) {
			c := testConfig(vocabs[0])
			blank(&c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("an empty %s is refused", field)
			}
			mustContain(t, "and it names "+field, err.Error(), field)
		})
	}
}

// ListNote and FreezeNote are the optional settings: podfreeze has nothing to add to the
// unknown-name error and lxcfreeze does. Empty must not be a refusal.
func TestValidateAcceptsEmptyOptionalNotes(t *testing.T) {
	c := testConfig(vocabs[0])
	c.ListNote, c.FreezeNote = "", ""
	if err := c.Validate(); err != nil {
		t.Fatalf("empty ListNote and FreezeNote are accepted: %v", err)
	}
}

// Collapsed into one word, every container would be both a freeze and a thaw target: the
// derived verb would always say freeze and the partition would never skip anything.
func TestValidateRefusesIdenticalStateWordsAndSaysWhich(t *testing.T) {
	c := testConfig(vocabs[0])
	c.StateFrozen = c.StateRunning
	err := c.Validate()
	if err == nil {
		t.Fatal("two identical state words are refused")
	}
	mustContain(t, "and it says which word they collapsed to", err.Error(), "running")
}

// The drill-down's "act on all of it" row shares a column with container names. Neither engine
// permits a name starting with '*', so a sentinel that does not begin with an alphanumeric can
// never collide with a real one.
func TestAllRowKeyCannotBeAContainerName(t *testing.T) {
	first := AllRowKey[0]
	alnum := first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first >= '0' && first <= '9'
	if alnum || !strings.HasPrefix(AllRowKey, "*") {
		t.Fatalf("%q starts with an alphanumeric", AllRowKey)
	}
}

// A bounded retry budget keeps a mistyped menu answer from looping for ever, and more than one
// try is what makes a typo recoverable (CLAUDE/InteractiveScripts.md).
func TestMaxTriesAllowsARecoverableMistake(t *testing.T) {
	if MaxTries < 2 {
		t.Fatalf("MaxTries is %d", MaxTries)
	}
}
