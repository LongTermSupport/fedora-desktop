// Package stt reads another program's state word, which a row of livenessAllowances excuses; any
// other liveness word here is still flagged.
package stt

var states = map[string]bool{"COMPLETE": true, "ACTIVE": false}

const shown = "online" // want `liveness word "online"`
