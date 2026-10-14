package freeze

import (
	"context"
	"errors"
)

// Loop is the interactive menu: choose a group, optionally narrow it, act, return to the menu,
// until the person quits. The inventory is re-read on every pass (the first pass reuses a load
// just made) so labels describe the machine now. The result is nil, or ErrSomeFailed when any
// act failed, even if the machine has since emptied or the person then quit.
func (s *Session[R]) Loop(ctx context.Context) error {
	var carried error
	for {
		stop, err := s.pass(ctx)
		if errors.Is(err, ErrSomeFailed) {
			carried, err = ErrSomeFailed, nil
		}
		if err != nil {
			return s.withWrites(err)
		}
		if stop {
			return s.withWrites(carried)
		}
	}
}

// pass is one trip round the menu. stop ends the session; a non-nil error is ErrSomeFailed
// (the session goes on) unless stop is also set.
func (s *Session[R]) pass(ctx context.Context) (bool, error) {
	if !s.fresh {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if err := s.Refresh(ctx); err != nil {
			return true, err
		}
	}
	s.fresh = false
	if s.inv.Len() == 0 {
		s.stderr.Printf("No running or frozen containers.\n")
		return true, nil
	}
	picked, err := s.choose(ctx)
	if errors.Is(err, ErrQuit) {
		return true, nil
	}
	if errors.Is(err, errAgain) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	err = s.DoAction(ctx, s.Action, picked)
	s.stderr.Printf("\n")
	if err != nil && !errors.Is(err, ErrSomeFailed) {
		return true, err
	}
	return false, err
}

// errAgain sends the loop back to the group menu: the choice came to nothing (the group went
// away, was empty, or the person backed out) and has already said so.
var errAgain = errors.New("back to the menu")

// choose takes the person from the group menu to a list of names, or to errAgain / ErrQuit.
func (s *Session[R]) choose(ctx context.Context) ([]string, error) {
	key, err := s.pickTarget(ctx)
	if err != nil {
		return nil, err
	}
	group, err := s.Engine.Select(ctx, s.inv, key)
	if errors.Is(err, ErrGroupGone) {
		s.stderr.Printf("\n")
		return nil, errAgain
	}
	if err != nil {
		return nil, selectFailure(err, key)
	}
	if len(group) == 0 {
		s.stderr.Printf("Nothing in that group.\n\n")
		return nil, errAgain
	}
	picked, err := s.drill(ctx, group)
	if errors.Is(err, ErrBack) {
		s.stderr.Printf("\n")
		return nil, errAgain
	}
	if gaveUp, ok := errors.AsType[*GaveUpError](err); ok {
		s.stderr.Printf("%s: %s\n\n", s.Cfg.Tool, gaveUp.Msg)
		return nil, errAgain
	}
	return picked, err
}

// selectFailure names a Select that broke. An engine's own fatal error already says what went
// wrong and passes through unchanged.
func selectFailure(err error, key string) error {
	if die, ok := errors.AsType[*DieError](err); ok && die != nil {
		return err
	}
	return Die("the select hook failed (%v) resolving '%s'", err, key)
}

// pickTarget shows the group menu and returns the chosen key, or ErrQuit.
func (s *Session[R]) pickTarget(ctx context.Context) (string, error) {
	keys, labels := s.menu()
	if len(keys) == 0 {
		return "", Die("internal error: the menu has no rows to offer.")
	}
	return s.Chooser.Group(ctx, keys, labels)
}

// drill narrows a group to the names the person wants: the whole group for the "all" row, or
// ErrBack. An empty group is back without asking anything.
func (s *Session[R]) drill(ctx context.Context, group []string) ([]string, error) {
	if len(group) == 0 {
		return nil, ErrBack
	}
	choice, err := s.Chooser.Members(ctx, s.screen(group))
	if err != nil {
		return nil, err
	}
	if choice.All {
		return group, nil
	}
	return choice.Names, nil
}
