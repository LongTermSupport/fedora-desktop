package declare

// The words a team's applied state is shown in, and the one for a thing that cannot be told.
const (
	WordUpToDate   = "up to date"
	WordNotBuilt   = "not built"
	WordRemoved    = "removed"
	WordUnreadable = "unreadable"
	WordUnknown    = "unknown"
)

// Word is a team's APPLIED column, in words.
func (s Status) Word() string {
	switch s.State {
	case ApplyCurrent:
		return WordUpToDate
	case ApplyOutdated:
		switch s.Detail {
		case "unstamped":
			return "out of date"
		case "config,software":
			return "out of date: both"
		default:
			return "out of date: " + s.Detail
		}
	case ApplyUnbuilt:
		return WordNotBuilt
	case ApplyRemoved:
		return WordRemoved
	case ApplyDecide:
		if s.Detail == "absent" {
			return "decide: absent"
		}
		return "decide: undeclared"
	case ApplyUnreadable:
		return WordUnreadable
	case ApplyAbsent:
		return WordUnknown
	}
	return WordUnknown
}
