package badproblem

import "fix/internal/teamchat/status"

func inATest(err error) status.Problem {
	return status.Problem{What: err.Error(), Fix: "x"} // want `an error's own text in a status problem`
}
