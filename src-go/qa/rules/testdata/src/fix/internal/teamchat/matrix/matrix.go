// Package matrix spells the Matrix specification's presence value, which a row of
// livenessAllowances excuses; any other liveness word here is still flagged.
package matrix

// Presence is the Matrix presence value, excused.
const Presence = "online"

// Shown is another liveness word, still flagged.
const Shown = "active" // want `liveness word "active"`
