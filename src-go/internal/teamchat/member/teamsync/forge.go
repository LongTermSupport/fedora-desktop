package teamsync

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The receive check's bounds (n3d-D29): at most forgeParallel checks at once, under one
// forgeBudget for all the checks of a batch, so a forge outage costs a team's sync at most the
// budget per batch.
const (
	forgeParallel = 8
	forgeBudget   = 30 * time.Second
)

// forgeNews is what a batch's checks leave for link.log: a check the forge answered (one that
// made a request; a cached or refused one shows nothing of the login), and one refused for want
// of a gh login.
type forgeNews struct{ answered, noLogin bool }

// forgeChecked is one check's result: the drop reason, "" for proven, and whether a sender's
// budget was charged for it, which only a check about to make a request is.
type forgeChecked struct {
	i       int
	reason  string
	err     error
	charged bool
}

// checkKey is a check's identity within a batch: the same reference, class and branches ask
// the forge the same questions, whatever the reference's text.
type checkKey struct {
	ref      proto.Ref
	class    proto.RefClass
	branch   string
	branches string
	listed   bool
}

func keyOf(c proto.ForgeCheck) checkKey {
	ref := c.Ref
	ref.Text = ""
	return checkKey{ref: ref, class: c.Class, branch: c.Branch, branches: strings.Join(c.Branches, "\x00"), listed: c.Branches != nil}
}

// spentChecks is each sender's uncached forge checks in the receive window (n3d-D31), under its
// own lock, since the checks spend from their own goroutines. It is in memory only: a link
// restart gives every sender a fresh window, as it does the flood window's copy in memory.
type spentChecks struct {
	mu sync.Mutex
	w  proto.Window
}

func (s *spentChecks) spend(sender string, nowMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w.Prune(proto.ReceiveWindowMs, nowMs)
	return s.w.Admit(sender, proto.ForgeChecksPerSender, proto.ReceiveWindowMs, nowMs)
}

// spendAny charges one check to the first of senders with budget left; false when none has.
func (s *spentChecks) spendAny(senders []string, nowMs int64) bool {
	return slices.ContainsFunc(senders, func(sender string) bool { return s.spend(sender, nowMs) })
}

// forgeCheck is SD5's receive check (A 9.3 "every receiver before it delivers"): each delivered
// verdict whose reference needs the forge is checked, concurrently, and one that does not
// prove becomes a drop with proto's reason. Identical checks are asked once (n3d-D31), charged
// to a sender of the group with budget left, and the answer is every one's; a group none of
// whose senders has budget is forge-rate with no request (n3d-D34). A check still running when
// the budget ends is unresolved, and each carries the budget as its bound, so no rate wait is
// slept past it. A Record answer is never checked (n3d-D25).
func (t *Team) forgeCheck(ctx context.Context, f proto.Forge, vs []proto.Verdict) forgeNews {
	groups := map[checkKey][]int{}
	var need []int
	for i, v := range vs {
		if v.Outcome != proto.Deliver || v.Forge == nil {
			continue
		}
		k := keyOf(*v.Forge)
		if groups[k] == nil {
			need = append(need, i)
		}
		groups[k] = append(groups[k], i)
	}
	if len(need) == 0 {
		return forgeNews{}
	}
	// The checks end before forgeCheck returns: the cancel below runs first, then the wait.
	var running sync.WaitGroup
	defer running.Wait()
	ctx, cancel := context.WithCancel(clock.WithBound(ctx, t.cfg.Clock.Boot()+forgeBudget))
	defer cancel()
	budget, stop := t.cfg.Clock.Hold(forgeBudget)
	defer stop()
	checks := make([]groupCheck, 0, len(need))
	for _, first := range need {
		var senders []string
		for _, i := range groups[keyOf(*vs[first].Forge)] {
			if s := vs[i].Event.Sender; !slices.Contains(senders, s) {
				senders = append(senders, s)
			}
		}
		checks = append(checks, groupCheck{first: first, need: *vs[first].Forge, senders: senders})
	}
	reasons, news := collect(t.startChecks(ctx, f, checks, &running), budget, need)
	for first, reason := range reasons {
		if reason == "" {
			continue
		}
		for _, i := range groups[keyOf(*vs[first].Forge)] {
			vs[i].Outcome, vs[i].Reason, vs[i].Answer = proto.Drop, reason, nil
		}
	}
	return news
}

// groupCheck is one check a batch asks: the first verdict of its group, what it checks, and the
// group's distinct senders in order, any of whom it may be charged to.
type groupCheck struct {
	first   int
	need    proto.ForgeCheck
	senders []string
}

// startChecks runs checks on running, forgeParallel at once, each sending its result; a check
// still waiting for its turn when ctx ends is unresolved. The channel holds every result, so a
// check that ends after the budget never blocks.
func (t *Team) startChecks(ctx context.Context, f proto.Forge, checks []groupCheck, running *sync.WaitGroup) <-chan forgeChecked {
	results := make(chan forgeChecked, len(checks))
	slots := make(chan struct{}, forgeParallel)
	for _, c := range checks {
		running.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- forgeChecked{i: c.first, reason: proto.ReasonUnresolved, err: ctx.Err()}
				return
			}
			defer func() { <-slots }()
			var charged atomic.Bool
			spend := func() bool {
				ok := t.spent.spendAny(c.senders, t.cfg.Clock.WallMS())
				charged.Store(ok)
				return ok
			}
			reason, err := t.checkOne(team.WithReceive(ctx, spend), c.need, f)
			results <- forgeChecked{i: c.first, reason: reason, err: err, charged: charged.Load()}
		})
	}
	return results
}

// collect is each check's reason as it comes, until the budget ends the rest unresolved.
func collect(results <-chan forgeChecked, budget <-chan time.Time, need []int) (map[int]string, forgeNews) {
	var news forgeNews
	reasons := make(map[int]string, len(need))
	for len(reasons) < len(need) {
		select {
		case r := <-results:
			reasons[r.i] = r.reason
			news.answered = news.answered || (r.err == nil && r.charged)
			news.noLogin = news.noLogin || errors.Is(r.err, team.ErrForgeLogin)
		case <-budget:
			for _, i := range need {
				if _, done := reasons[i]; !done {
					reasons[i] = proto.ReasonUnresolved
				}
			}
		}
	}
	return reasons, news
}

// checkOne is one reference's check: "" for proven, else the drop reason; an outcome with no
// name here, and every error, is unresolved.
func (t *Team) checkOne(ctx context.Context, need proto.ForgeCheck, f proto.Forge) (string, error) {
	if t.cfg.Forge == nil {
		return proto.ReasonUnresolved, errors.New("teamsync: no forge checker")
	}
	res, err := t.cfg.Forge.Check(ctx, need, f)
	if err != nil {
		return proto.ReasonUnresolved, err
	}
	switch res.Outcome {
	case team.ForgeProven:
		return "", nil
	case proto.ReasonProvenance, proto.ReasonUnresolved, proto.ReasonForgeRate:
		return res.Outcome, nil
	}
	return proto.ReasonUnresolved, nil
}

// noteForge says forge-login once per team until a check is next answered (n3d-D10).
func (t *Team) noteForge(news forgeNews) {
	t.mu.Lock()
	say := news.noLogin && !t.loginSaid
	t.loginSaid = (t.loginSaid || news.noLogin) && !news.answered
	t.mu.Unlock()
	if say {
		t.log.Printf("the forge checks have no gh login (forge-login): referenced pings are dropped unresolved")
	}
}
