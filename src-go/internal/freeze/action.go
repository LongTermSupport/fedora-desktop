package freeze

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Session is one run of a freeze tool: the engine, the person (a Chooser), and the options the
// command line set. It owns the current inventory.
type Session struct {
	Cfg     Config
	Env     Env
	Engine  Engine
	Chooser Chooser
	// Action is the explicit verb from the command line; VerbDerived lets the state decide.
	Action Verb
	// DryRun previews every act on stdout instead of performing it.
	DryRun bool

	inv    Inventory
	fresh  bool // the inventory was just loaded and not yet used by the menu
	stdout *term.Term
	stderr *term.Term
}

// NewSession wires a session. Out is the payload stream (list, dry run); Err takes everything
// else.
func NewSession(cfg Config, env Env, engine Engine, chooser Chooser) *Session {
	return &Session{
		Cfg: cfg, Env: env, Engine: engine, Chooser: chooser,
		stdout: &term.Term{Err: env.Out}, stderr: &term.Term{Err: env.Err},
	}
}

// Refresh reads the engine's state afresh. The menu loop reuses a load made just before it
// rather than costing the engine a second round trip.
func (s *Session) Refresh(ctx context.Context) error {
	inv, err := s.Engine.Load(ctx)
	if err != nil {
		return err
	}
	s.inv, s.fresh = inv, true
	return nil
}

// Inventory is the current state.
func (s *Session) Inventory() Inventory { return s.inv }

// DoAction freezes or thaws the selected names. VerbDerived lets the state decide. There is no
// confirmation prompt: freezing is undone by doing it again. Every target is attempted and each
// failure is named with the engine's own words; any failure ends in ErrSomeFailed.
func (s *Session) DoAction(ctx context.Context, verb Verb, selected []string) error {
	return s.withWrites(s.doAction(ctx, verb, selected))
}

func (s *Session) doAction(ctx context.Context, verb Verb, selected []string) error {
	if len(selected) == 0 {
		return Die("nothing matched — the selection is empty.\n  Run '%s list' to see what is running and what is frozen.", s.Cfg.Tool)
	}
	if verb == VerbDerived {
		verb = s.inv.InferAction(selected)
	}
	wanted, past := s.Cfg.StateFrozen, "THAWED"
	if verb == VerbFreeze {
		wanted, past = s.Cfg.StateRunning, "FROZEN"
	}
	b := s.inv.Partition(verb, selected)
	if len(b.Vanished) > 0 {
		s.stderr.Printf("Skipped — no longer present (gone since the inventory was read): %s\n", strings.Join(b.Vanished, " "))
	}
	if len(b.Skipped) > 0 {
		s.stderr.Printf("Skipped — not currently %s: %s\n", wanted, strings.Join(b.Skipped, " "))
	}
	if len(b.Act) == 0 {
		s.stderr.Printf("Nothing to do — every selected container is already in the requested state.\n")
		return nil
	}
	if s.DryRun {
		s.stdout.Printf("DRY RUN — %d container(s) would be %s:\n%s", len(b.Act), past, s.Table(b.Act...))
		return nil
	}
	s.stderr.Printf("%d container(s) being %s:\n%s", len(b.Act), past, s.Table(b.Act...))
	done, failed := s.actOn(ctx, verb, b.Act)
	if verb == VerbFreeze && len(done) > 0 {
		s.stderr.Printf("\n  Thaw them with: %s thaw %s\n", s.Cfg.Tool, strings.Join(done, " "))
		if s.Cfg.FreezeNote != "" {
			s.stderr.Printf("\n%s\n", s.Cfg.FreezeNote)
		}
	}
	if failed > 0 {
		s.stderr.Printf("%s: %d of %d container(s) failed — see above.\n", s.Cfg.Tool, failed, len(b.Act))
		return ErrSomeFailed
	}
	return nil
}

// actOn attempts every target in order, reporting each, and returns the names that succeeded and
// the number that failed.
func (s *Session) actOn(ctx context.Context, verb Verb, names []string) ([]string, int) {
	var done []string
	failed := 0
	for _, name := range names {
		out, err := s.Engine.Act(ctx, verb, name)
		if err != nil {
			text := strings.TrimRight(out, "\n")
			if text == "" {
				text = err.Error()
			}
			s.stderr.Printf("  ✗ %s — %s\n", name, text)
			failed++
			continue
		}
		s.stderr.Printf("  ✓ %s\n", name)
		done = append(done, name)
	}
	return done, failed
}

// withWrites folds a failed write to either stream into the error a run ends with, so a full disk
// or a closed pipe is surfaced rather than lost.
func (s *Session) withWrites(err error) error {
	for _, t := range []*term.Term{s.stdout, s.stderr} {
		if t.WriteErr != nil && err == nil {
			err = fmt.Errorf("write output: %w", t.WriteErr)
		}
		t.WriteErr = nil
	}
	return err
}
