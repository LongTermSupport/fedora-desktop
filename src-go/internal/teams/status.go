package teams

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"

// The APPLIED words more than one flow tests for.
const (
	wordUpToDate = "up to date"
	wordNotBuilt = "not built"
	wordRemoved  = "removed"
)

// applyWord is a team's APPLIED column, in words.
func applyWord(st declare.Status) string {
	switch st.State {
	case declare.ApplyCurrent:
		return wordUpToDate
	case declare.ApplyOutdated:
		switch st.Detail {
		case "unstamped":
			return "out of date"
		case "config,software":
			return "out of date: both"
		default:
			return "out of date: " + st.Detail
		}
	case declare.ApplyUnbuilt:
		return wordNotBuilt
	case declare.ApplyRemoved:
		return wordRemoved
	case declare.ApplyDecide:
		if st.Detail == "absent" {
			return "decide: absent"
		}
		return "decide: undeclared"
	case declare.ApplyUnreadable:
		return wordUnreadable
	case declare.ApplyAbsent:
		return wordUnknown
	}
	return wordUnknown
}
