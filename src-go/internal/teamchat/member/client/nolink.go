package client

import (
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// B 2.4.2's recovery texts, by keeper.json's kind; the session name is the one fill.
const (
	msgRestarting exits.Template = "teamchat is restarting (next try in {} s); team messages wait on the server"
	msgStarting   exits.Template = "teamchat is starting or restarting; team messages wait on the server"
	msgStoppedCCY exits.Template = "teamchat has stopped in this session; team messages are not received; ask the owner to run `ccy-teams restart {}`"
	msgStopped    exits.Template = "teamchat has stopped in this session; end this session and launch it again"
)

// noLink is LN3's answer when no link answered: keeper.lock held and the link restarting or
// running is no-link (with the restart time when restarting); a failed link, or keeper.lock
// free (the keeper has gone), is link-failed with the kind's recovery. It reads two files and
// waits on nothing.
func (c conn) noLink() *exits.Error {
	root, err := homefs.Open(c.run.Dir(), homefs.Private)
	if err != nil {
		return c.failed(nil)
	}
	defer func() {
		if err := root.Close(); err != nil {
			return
		}
	}()
	held, heldErr := lock.Probe(root, c.run.KeeperLock())
	k, kErr := state.Read(root, c.run.KeeperJSON(), state.MaxKeeper, state.DecodeKeeper)
	var known *state.Keeper
	if kErr == nil {
		known = &k
	}
	switch {
	case heldErr != nil || !held || known != nil && known.Link == state.LinkFailed:
		return c.failed(known)
	case known != nil && known.Link == state.LinkRestarting:
		left := time.Duration(*known.NextMS)*time.Millisecond - c.clock.Boot()
		secs := max(0, (left+time.Second-time.Millisecond)/time.Second)
		return exits.New(exits.CodeNoLink, c.team, msgRestarting, exits.Count(int64(secs)))
	}
	return exits.New(exits.CodeNoLink, c.team, msgStarting)
}

// failed is link-failed with the recovery for k's kind; with no keeper.json, a session's own.
func (c conn) failed(k *state.Keeper) *exits.Error {
	if k != nil && (k.Kind == state.KindCCY || k.Kind == state.KindLXC) {
		name := ""
		if k.Session != nil {
			name = k.Session.String()
		}
		return exits.New(exits.CodeLinkFailed, c.team, msgStoppedCCY, exits.Name(name))
	}
	return exits.New(exits.CodeLinkFailed, c.team, msgStopped)
}
