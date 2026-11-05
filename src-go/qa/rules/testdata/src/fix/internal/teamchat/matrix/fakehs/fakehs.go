// Package fakehs answers a presence request with the Matrix specification's value, which a row
// of livenessAllowances excuses; any other liveness word here is still flagged.
package fakehs

// Presence is the Matrix presence value, excused.
const Presence = "online"

// Shown is another liveness word, still flagged.
const Shown = "alive" // want `liveness word "alive"`
