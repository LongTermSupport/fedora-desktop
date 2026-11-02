// Package systemd owns a unit's ActiveState words: systemd's own answer of whether it runs.
package systemd

// Up is whether a unit's ActiveState is running.
func Up(state string) bool { return state == "active" }
