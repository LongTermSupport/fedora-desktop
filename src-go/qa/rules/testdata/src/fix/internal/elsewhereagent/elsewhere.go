// Package elsewhereagent is outside teamchat: the rule does not judge it.
package elsewhereagent

import (
	"fix/internal/teamchat/member/state"
	"fix/internal/teamchat/member/wire"
)

func plain(l wire.Launch) bool { return l.Launch == state.LaunchPlain }
