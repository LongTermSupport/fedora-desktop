// Command teamchat-admin stands for teamchat's admin command: it is in the rule's scope.
package main

import (
	"os"

	"fix/internal/teamchat/member/state"
)

func main() {
	var p state.SessionPID
	if p.Launch != "claude" { // want `the launch field compared with "claude" outside package agent`
		os.Exit(1)
	}
}
