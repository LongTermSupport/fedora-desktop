package engine

import "qres/internal/teamchat/member/records"

// A test builds a record as it expects it: not flagged.
func expected() records.Human { return records.Human{ID: "x"} }
