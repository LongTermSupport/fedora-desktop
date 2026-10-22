package teamsync

import (
	"context"
	"errors"
	"time"

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

// forgeNews is what a batch's checks leave for link.log: a check the forge answered, and one
// refused for want of a gh login.
type forgeNews struct{ answered, noLogin bool }

// forgeChecked is one check's result: the drop reason, "" for proven.
type forgeChecked struct {
	i      int
	reason string
	err    error
}

// forgeCheck is SD5's receive check (A 9.3 "every receiver before it delivers"): each delivered
// verdict whose reference needs the forge is checked, concurrently, and one that does not
// prove becomes a drop with proto's reason. A check still running when the budget ends is
// unresolved. A Record answer is never checked (n3d-D25).
func (t *Team) forgeCheck(ctx context.Context, f proto.Forge, vs []proto.Verdict) forgeNews {
	var need []int
	for i, v := range vs {
		if v.Outcome == proto.Deliver && v.Forge != nil {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return forgeNews{}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	budget, stop := t.cfg.Clock.Hold(forgeBudget)
	defer stop()
	reasons, news := collect(t.startChecks(ctx, f, vs, need), budget, need)
	for i, reason := range reasons {
		if reason != "" {
			vs[i].Outcome, vs[i].Reason, vs[i].Answer = proto.Drop, reason, nil
		}
	}
	return news
}

// startChecks runs the checks of need, forgeParallel at once, each sending its result; a check
// still waiting for its turn when ctx ends is unresolved. The channel holds every result, so a
// check that ends after the budget never blocks.
func (t *Team) startChecks(ctx context.Context, f proto.Forge, vs []proto.Verdict, need []int) <-chan forgeChecked {
	results := make(chan forgeChecked, len(need))
	slots := make(chan struct{}, forgeParallel)
	for _, i := range need {
		check := *vs[i].Forge
		go func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- forgeChecked{i: i, reason: proto.ReasonUnresolved, err: ctx.Err()}
				return
			}
			defer func() { <-slots }()
			reason, err := t.checkOne(ctx, check, f)
			results <- forgeChecked{i: i, reason: reason, err: err}
		}()
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
			news.answered = news.answered || r.err == nil
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

// noteForge says forge-login once per link until a check is next answered (n3d-D10).
func (t *Team) noteForge(news forgeNews) {
	t.mu.Lock()
	say := news.noLogin && !t.loginSaid
	t.loginSaid = (t.loginSaid || news.noLogin) && !news.answered
	t.mu.Unlock()
	if say {
		t.log.Printf("the forge checks have no gh login (forge-login): referenced pings are dropped unresolved")
	}
}
