package teams

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"

// The applied states declare.py status reports.
const (
	stCurrent    = "current"
	stOutdated   = "outdated"
	stUnbuilt    = "unbuilt"
	stRemoved    = "removed"
	stDecide     = "decide"
	stUnreadable = "unreadable"
)

// The APPLIED words more than one flow tests for.
const (
	wordUpToDate = "up to date"
	wordNotBuilt = "not built"
	wordRemoved  = "removed"
)

// applyWord is a team's APPLIED column, in words.
func applyWord(st declare.Status) string {
	switch st.State {
	case stCurrent:
		return wordUpToDate
	case stOutdated:
		switch st.Detail {
		case "unstamped":
			return "out of date"
		case "config,software":
			return "out of date: both"
		default:
			return "out of date: " + st.Detail
		}
	case stUnbuilt:
		return wordNotBuilt
	case stRemoved:
		return wordRemoved
	case stDecide:
		if st.Detail == "absent" {
			return "decide: absent"
		}
		return "decide: undeclared"
	case stUnreadable:
		return wordUnreadable
	}
	return wordUnknown
}
