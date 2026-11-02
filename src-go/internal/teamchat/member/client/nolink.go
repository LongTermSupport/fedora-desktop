package client

import (
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// B 2.4.2's recovery texts, by keeper.json's kind; the session name is the one fill.
const (
	msgRestarting exits.Template = "teamchat is restarting (next try in {} s); team messages wait on the server"
	msgStarting   exits.Template = "teamchat is starting or restarting; team messages wait on the server"
	msgStoppedCCY exits.Template = "teamchat has stopped in this session; team messages are not received; ask the owner to run `ccy-teams restart {}`"
	msgStopped    exits.Template = "teamchat has stopped in this session; end this session and launch it again"
)

// The states of a link that does not answer.
const (
	DownRestarting = "restarting"
	DownStarting   = "starting"
	DownFailed     = "failed"
)

// Down is why no link answers, as keeper.json and the keeper.lock probe tell it (LN3): the
// state, the restart's CLOCK_BOOTTIME ms when restarting, and the member kind and session
// name the recovery needs.
type Down struct {
	State   string
	NextMS  int64
	Kind    string
	Session string
}

// ReadDown is LN3's reading: keeper.lock held and the link restarting is restarting, held and
// anything else but failed is starting; a failed link, or keeper.lock free (the keeper has
// gone), is failed. It reads one file, probes one lock, waits on nothing and never fails: a
// run directory it cannot open is the keeper gone.
func ReadDown(run paths.Run) Down {
	root, err := homefs.Open(run.Dir(), homefs.Private)
	if err != nil {
		return Down{State: DownFailed}
	}
	defer func() {
		if err := root.Close(); err != nil {
			return
		}
	}()
	held, heldErr := lock.Probe(root, run.KeeperLock())
	k, kErr := state.Read(root, run.KeeperJSON(), state.MaxKeeper, state.DecodeKeeper)
	d := Down{State: DownStarting}
	known := kErr == nil
	if known {
		d.Kind = k.Kind
		if k.Session != nil {
			d.Session = k.Session.String()
		}
	}
	switch {
	case heldErr != nil || !held || known && k.Link == state.LinkFailed:
		d.State = DownFailed
	case known && k.Link == state.LinkRestarting && k.NextMS != nil:
		d.State, d.NextMS = DownRestarting, *k.NextMS
	}
	return d
}

// Error is the verbs' exit 69 for d at boot (CLOCK_BOOTTIME): no-link while restarting (with
// the whole seconds left) or starting, link-failed with the kind's recovery otherwise.
func (d Down) Error(team *paths.TeamID, boot time.Duration) *exits.Error {
	switch d.State {
	case DownRestarting:
		left := time.Duration(d.NextMS)*time.Millisecond - boot
		secs := max(0, (left+time.Second-time.Millisecond)/time.Second)
		return exits.New(exits.CodeNoLink, team, msgRestarting, exits.Count(int64(secs)))
	case DownStarting:
		return exits.New(exits.CodeNoLink, team, msgStarting)
	}
	if d.Kind == state.KindCCY || d.Kind == state.KindLXC {
		return exits.New(exits.CodeLinkFailed, team, msgStoppedCCY, exits.Name(d.Session))
	}
	return exits.New(exits.CodeLinkFailed, team, msgStopped)
}

// noLink is LN3's answer when no link answered.
func (c conn) noLink() *exits.Error {
	return ReadDown(c.run).Error(c.team, c.clock.Boot())
}
