// Package paths stands for teamchat's paths: the machine's fixed record paths, and a name that
// is not one.
package paths

// StatusRoot is where the machine's status records are.
const StatusRoot = "/var/lib/teamchat-status"

// LXCRecordDir holds the LXC records.
const LXCRecordDir = StatusRoot + "/lxc"

// StatusFile is a team's status record.
func StatusFile(team string) string { return StatusRoot + "/teams/" + team + "/status.json" }

// StampFile is a team's install stamp.
func StampFile(team string) string { return StatusRoot + "/teams/" + team + "/stamp.json" }

// SeatName is not a machine record.
func SeatName(seat string) string { return "seat-" + seat }
