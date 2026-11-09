// Command teamchat stands for teamchat's command: it is in the rule's scope.
package main

import (
	"os"

	"fix/internal/teamchat/member/wire"
)

func main() {
	var l wire.Launch
	if l.Launch == "plain" { // want `the launch field compared with "plain" outside package agent`
		os.Exit(1)
	}
}
