package badlive

import "fix/internal/agentbus"

// account is decoded from JSON: a struct tag names a field, it is shown to nobody.
type account struct {
	Active bool `json:"active"`
}

// Words that claim no liveness, words that hold one inside a sentence, and the bus's own word
// through its owner are clean.
func clean(a account) []string {
	return []string{"inactive", "parked", "offline", "stale", "listening since noon", "not active",
		agentbus.Listening, map[bool]string{true: "yes"}[a.Active]}
}
