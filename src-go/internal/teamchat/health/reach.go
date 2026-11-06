package health

import (
	"context"
	"errors"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// reaching is X32's probes of one team through its terminator, one goroutine per listen address,
// each bounded by matrix.ReachLimit on the tick's timers: a slow address delays neither another's
// probe nor the tick's other work, which runs while they do. Each probe signals its end through
// wg. A nil reaching is no probe.
type reaching struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// found is each address's problem, in the reach's order; set only before wg ends.
	found []*status.Problem
	// fixed is a problem known before any probe, the reach that could not be read.
	fixed *status.Problem
}

// startReach starts the probes of each listen address the team's reach names.
func (t *Tick) startReach(ctx context.Context, team paths.TeamID) *reaching {
	r := &reaching{}
	reach, err := t.cfg.Reach(team)
	if err != nil {
		r.fixed = &status.Problem{
			What: "the TLS terminator cannot be probed: its listen addresses or the team's CA certificate cannot be read",
			Fix:  "teamchat-admin health " + team.String() + ", then apply the team again (" + paths.PlayFile + ")",
		}
		return r
	}
	probeCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.found = make([]*status.Problem, len(reach.Addrs))
	fix := "systemctl status " + paths.TLSUnit(team)
	wall := wallOf{t.cfg.Clock}
	for i, addr := range reach.Addrs {
		r.wg.Go(func() {
			err := matrix.Reach(probeCtx, matrix.ReachConfig{Addr: addr, CAPEM: string(reach.CAPEM), Wall: wall, Timers: t.cfg.Timers})
			if err == nil {
				return
			}
			r.found[i] = &status.Problem{What: "the TLS terminator does not answer on " + addr.String() + ": " + reachCause(err), Fix: fix}
		})
	}
	return r
}

// reachCause is a failed probe's fixed cause: the transport's own, or, for a probe the tick's
// context ended, that it did not finish.
func reachCause(err error) string {
	if re, ok := errors.AsType[*matrix.ReachError](err); ok {
		return re.Cause
	}
	return "the probe did not finish"
}

// collect waits for every probe and gives their problems, in the reach's order.
func (r *reaching) collect() []status.Problem {
	if r == nil {
		return nil
	}
	r.wg.Wait()
	var out []status.Problem
	if r.fixed != nil {
		out = append(out, *r.fixed)
	}
	for _, p := range r.found {
		if p != nil {
			out = append(out, *p)
		}
	}
	r.abandon()
	return out
}

// abandon ends every probe still running and waits for each to end: a tick that failed after the
// probes started leaves none behind. It is safe after collect, and on a nil reaching.
func (r *reaching) abandon() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// wallOf is the tick's clock as the wall time a certificate's validity is judged by.
type wallOf struct{ clock Clock }

func (w wallOf) WallMS() int64 { return w.clock.Now().UnixMilli() }

var _ matrix.WallClock = wallOf{}
