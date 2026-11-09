// Package agentbus owns the bus's liveness words: it reads them from the member's status.
package agentbus

// Listening is a member whose status lease runs past now.
const Listening = "listening"

// notParked is the registry's park word, which agent-bus list prints.
const notParked = "active"

// Parked is whether a park word is parked.
func Parked(word string) bool { return word != notParked }
