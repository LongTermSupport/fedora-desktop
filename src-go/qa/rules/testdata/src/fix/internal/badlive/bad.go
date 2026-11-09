// Package badlive shows a member as alive from a source that says nothing of whether it runs.
package badlive

// member is a registry entry: parked or not, nothing about a running session.
type member struct {
	handle string
	parked bool
}

// The original: every member not parked was called "active", which reads as running.
func states(members []member) map[string]string {
	out := map[string]string{}
	for _, m := range members {
		out[m.handle] = "active" // want `liveness word "active"`
		if m.parked {
			out[m.handle] = "parked"
		}
	}
	return out
}

// A liveness word kept in a constant is the same claim.
const (
	stateOnline = "online"    // want `liveness word "online"`
	wordUp      = "Listening" // want `liveness word "Listening"`
)

func colour(word string) bool {
	switch word {
	case "alive", " active ": // want `liveness word "alive"` `liveness word " active "`
		return true
	}
	return word == stateOnline || word == wordUp
}

var labels = []string{"ONLINE"} // want `liveness word "ONLINE"`

func raw() string {
	return `listening` // want "liveness word \"listening\""
}
