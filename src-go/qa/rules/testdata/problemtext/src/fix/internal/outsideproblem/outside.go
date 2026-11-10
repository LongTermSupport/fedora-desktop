// Package outsideproblem is outside teamchat: the rule does not judge it.
package outsideproblem

import "fix/internal/teamchat/status"

func outside(err error) status.Problem { return status.Problem{What: err.Error(), Fix: "x"} }
