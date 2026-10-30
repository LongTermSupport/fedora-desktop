package containerimages

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// removeWhy is what the removal log says of each old-range removal, before the item's own why.
const removeWhy = "Plan 00235 old-range decision: "

// stillSafe is O15's fresh look before any removal: a container to remove is still listed and
// not running, and no container the plan does not remove uses an image or mounts a volume it
// removes.
func stillSafe(p Plan, pv podmanView) error {
	removing := map[string]bool{}
	for _, s := range p.Remove {
		if s.Kind == kindCtr {
			removing[s.Key] = true
		}
	}
	errs := make([]error, 0, len(p.Remove))
	for _, s := range p.Remove {
		errs = append(errs, stepSafe(s, pv, removing))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("the store changed since the plan, so nothing is removed; triage again:\n%w", err)
	}
	return nil
}

// stepSafe is stillSafe for one step; removing is the containers the plan removes.
func stepSafe(s PlanStep, pv podmanView, removing map[string]bool) error {
	var users []string
	for _, id := range slices.Sorted(maps.Keys(pv.containers)) {
		c := pv.containers[id]
		uses := s.Kind == kindImage && c.ImageID == s.Key || s.Kind == kindVolume && slices.Contains(pv.mounts[id], s.Key)
		if uses && !removing[id] {
			users = append(users, c.Name)
		}
	}
	c, listed := pv.containers[s.Key]
	switch {
	case s.Kind == kindCtr && !listed:
		return fmt.Errorf("container %s is no longer listed by podman", s.Name)
	case s.Kind == kindCtr && !slices.Contains(deadStates, c.State):
		return fmt.Errorf("container %s is %s now", s.Name, c.State)
	case s.Kind != kindCtr && s.Kind != kindImage && s.Kind != kindVolume:
		return fmt.Errorf("the plan's %s %s is not podman's to remove", s.Kind, s.Key)
	case len(users) > 0:
		return fmt.Errorf("%s %s is used by container %s now", s.Kind, s.Name, strings.Join(users, ", "))
	}
	return nil
}

// removeStep is one of podman's own removals: unforced, but for a container another tool made.
func removeStep(ctx context.Context, eng engine.Engine, s PlanStep) error {
	switch {
	case s.Kind == kindCtr && s.External:
		return eng.RemoveExternalContainer(ctx, s.Key)
	case s.Kind == kindCtr:
		return eng.RemoveContainer(ctx, s.Key)
	case s.Kind == kindImage:
		return eng.RemoveImage(ctx, s.Key)
	default:
		return eng.RemoveVolume(ctx, s.Key)
	}
}

// removeOld is `old-range remove --plan FILE [--yes]` (O15): as the plan's user, podman removes
// each thing the plan removes, in its order, once a fresh look finds it still safe; each
// removal is logged once it went. A dry run without --yes.
func (a App) removeOld(ctx context.Context, planPath string, yes bool) term.ExitCode {
	if err := a.notRoot("remove"); err != nil {
		return a.fail(err)
	}
	p, err := readPlan(planPath)
	if err != nil {
		return a.fail(err)
	}
	if err = a.asUser(p.Owners.UID); err != nil {
		return a.fail(err)
	}
	eng, err := engine.New(engine.Podman, a.Env.Runner)
	if err != nil {
		return a.fail(err)
	}
	r := &run{App: a, eng: eng, yes: yes}
	if yes {
		release, lockErr := a.takeRunLock()
		if lockErr != nil {
			return a.fail(lockErr)
		}
		defer release()
	}
	pv, err := a.podmanView(ctx)
	if err != nil {
		return a.fail(err)
	}
	if p.Remove, err = r.dropGone(ctx, p); err != nil {
		return a.fail(err)
	}
	if err = stillSafe(p, pv); err != nil {
		return a.fail(err)
	}
	if !yes {
		r.p.Print(a.Env.Stdout, "old-range remove: a dry run, nothing is removed. --yes removes what is listed.\n")
		return r.finish(r.sayRemovals(p))
	}
	if r.log, err = a.openLog(); err != nil {
		return a.fail(err)
	}
	code := r.removeAll(ctx, p)
	if err := r.log.Close(); err != nil {
		return a.fail(fmt.Errorf("close the removal log %s: %w", r.log.Name(), err))
	}
	return code
}

// dropGone is O15's look before any removal: each step podman no longer has is already gone,
// named and dropped, and the steps left are returned. The plan's own done items are named too.
func (r *run) dropGone(ctx context.Context, p Plan) ([]PlanStep, error) {
	for _, line := range p.Done {
		r.p.Print(r.Env.Stdout, line+"\n")
	}
	left := make([]PlanStep, 0, len(p.Remove))
	for _, s := range p.Remove {
		there, err := r.eng.StoreHas(ctx, s.Kind, s.Key)
		if err != nil {
			return nil, err
		}
		if !there {
			r.p.Print(r.Env.Stdout, "already gone: "+s.Kind+" "+s.Name+"\n")
			continue
		}
		left = append(left, s)
	}
	return left, nil
}

// sayRemovals is the dry run's list.
func (r *run) sayRemovals(p Plan) term.ExitCode {
	for _, s := range p.Remove {
		r.p.Print(r.Env.Stdout, "would remove "+s.Kind+" "+s.Name+": "+s.Why+"\n")
	}
	r.sayLeft(p)
	return term.ExitOK
}

func (r *run) sayLeft(p Plan) {
	for _, left := range p.Left {
		r.p.Print(r.Env.Stdout, "left: "+left+"\n")
	}
}

// takenEarlier is O15 after a failed removal: podman, asked, no longer has the item, so an
// earlier removal of this run took it with it (an untagged parent goes with the image built on
// it). Never podman's error text: podman is asked.
func (r *run) takenEarlier(ctx context.Context, s PlanStep) (bool, error) {
	there, err := r.eng.StoreHas(ctx, s.Kind, s.Key)
	return err == nil && !there, err
}

// removeAll removes each step: a refusal is named and the rest still go; P6a's stops the run. A
// failed removal of what podman no longer has is done, logged as any other.
func (r *run) removeAll(ctx context.Context, p Plan) term.ExitCode {
	for _, s := range p.Remove {
		err := removeStep(ctx, r.eng, s)
		said := "removed "
		switch {
		case err != nil && deniedInTempdirs(err):
			r.failed = append(r.failed, "stopped at the removal of "+s.Kind+" "+s.Name+": the store holds "+storeCause+
				"; this run removes nothing more. podman said: "+err.Error())
			return r.finish(term.ExitFailure)
		case err != nil:
			taken, askErr := r.takenEarlier(ctx, s)
			if !taken {
				r.failed = append(r.failed, "could not remove "+s.Kind+" "+s.Name+": "+errors.Join(err, askErr).Error())
				continue
			}
			said = "removed with an earlier item: "
		}
		r.p.Print(r.Env.Stdout, said+s.Kind+" "+s.Name+": "+s.Why+"\n")
		if err := r.record(action{kind: s.Kind, name: s.Name, id: s.Key, why: removeWhy + s.Why}); err != nil {
			return r.fail(err)
		}
	}
	r.sayLeft(p)
	return r.finish(term.ExitOK)
}
