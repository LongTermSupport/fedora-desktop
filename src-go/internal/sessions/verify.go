package sessions

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

var wholeSeconds = regexp.MustCompile(`^[0-9]+$`)

// parseVerify reads `verify-restore [--wait SECONDS]`; a mistake is returned as its sentence.
func parseVerify(args []string) (time.Duration, string) {
	var wait time.Duration
	for i := 0; i < len(args); i++ {
		if args[i] != "--wait" {
			return 0, fmt.Sprintf("unknown verify-restore option '%s'.", args[i])
		}
		value := ""
		if i+1 < len(args) {
			value = args[i+1]
		}
		secs, err := strconv.Atoi(value)
		if !wholeSeconds.MatchString(value) || err != nil {
			return 0, fmt.Sprintf("--wait needs a whole number of seconds; got '%s'.", value)
		}
		wait = time.Duration(secs) * time.Second
		i++
	}
	return wait, ""
}

// verifyRestore checks every session this boot's restore brought up, one line each on stdout:
// "<name> OK", "<name> STARTING", "<name> SETTING-GOING", "<name> NOT-SET-GOING <why>",
// "<name> WAITING-AT-PROMPT <which>" or "<name> DEAD <why>". Exit 0 only if all are OK. --wait
// polls until the restore has settled, or the time is up.
func (a *App) verifyRestore(ctx context.Context, args []string) int {
	wait, problem := parseVerify(args)
	if problem != "" {
		return a.usageError(problem)
	}
	regdir, err := registry.Dir(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	store := restore.NewStore(regdir, a.cfg.BootIDFile)
	m, err := store.Read()
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	boot, err := store.BootID()
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	if m.Boot != boot {
		a.fail("the last session restore ran in an earlier boot, so it says nothing about this one; nothing restored these sessions since this machine started.")
		return term.ExitFailure
	}
	if len(m.Entries) == 0 {
		a.sayf("This boot's restore brought up no sessions; nothing to verify.\n")
		return term.ExitOK
	}
	states, err := a.settledStates(ctx, store, wait)
	if err != nil {
		return term.ExitFailure
	}
	a.out(strings.Join(states, "\n") + "\n")
	if !allOK(states) {
		a.sayf("Not every restored session is running and past its prompts; attach with ccy-sessions to answer or look.\n")
		return term.ExitFailure
	}
	return term.ExitOK
}

// settledStates looks again until the restore has settled: every session OK, or none still
// STARTING or SETTING-GOING and the same report read twice running, so a prompt seen for one
// poll while the launcher answers it is not taken for where the session stopped. The manifest
// is read afresh each time, because set-going writes to it meanwhile. With no time to wait it
// looks once.
func (a *App) settledStates(ctx context.Context, store *restore.Store, wait time.Duration) ([]string, error) {
	deadline := a.Now().Add(wait)
	previous := ""
	for {
		m, err := store.Read()
		if err != nil {
			a.failErr(err)
			return nil, errReported
		}
		states, err := a.restoreStates(ctx, m)
		if err != nil {
			return nil, err
		}
		joined := strings.Join(states, "\n")
		if allOK(states) || !a.Now().Before(deadline) || (joined == previous && !transient(states)) {
			return states, nil
		}
		previous = joined
		a.Pause(a.cfg.Poll, nil)
	}
}

func allOK(states []string) bool {
	return !slices.ContainsFunc(states, func(s string) bool { return !strings.HasSuffix(s, " OK") })
}

func transient(states []string) bool {
	return slices.ContainsFunc(states, func(s string) bool {
		return strings.HasSuffix(s, " STARTING") || strings.HasSuffix(s, " SETTING-GOING")
	})
}

// hostView is what one pass over the host found: which sessions are live, the container each
// session's engine client names, and the containers that are running.
type hostView struct {
	live     map[string]bool
	clientOf map[string]string
	running  map[string]bool
}

// hostView reads the live list, the process table and each engine in use.
func (a *App) hostView(ctx context.Context) (hostView, error) {
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		return hostView{}, errReported
	}
	v := hostView{live: map[string]bool{}, clientOf: map[string]string{}}
	for _, s := range sessions {
		v.live[s.Name] = true
	}
	owners, err := a.sessionContainers(ctx)
	if err != nil {
		return hostView{}, err
	}
	var engines []string
	for _, o := range owners {
		if o.HasContainer() {
			v.clientOf[o.Session] = o.Container
			if !slices.Contains(engines, o.Engine) {
				engines = append(engines, o.Engine)
			}
		}
	}
	v.running, err = a.runningContainers(ctx, engines)
	return v, err
}

// containerState is "-" for a session with no engine client, "up" when its container is in the
// engine's running list, else "starting".
func (v hostView) containerState(name string) string {
	client := v.clientOf[name]
	switch {
	case client == "":
		return "-"
	case v.running[client]:
		return "up"
	}
	return "starting"
}

// restoreStates is "<name> <STATE>[ <detail>]" for every session in the manifest, from one pass
// over the host: the live list, one screen capture per live session, one process table, one
// engine query per engine a session uses, and the transcript of each session set going. A probe
// that fails is a failure, never a state: "could not look" must not read as "dead" or as "fine".
func (a *App) restoreStates(ctx context.Context, m restore.Manifest) ([]string, error) {
	view, err := a.hostView(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		state, stateErr := a.stateOf(ctx, e, view)
		if stateErr != nil {
			return nil, stateErr
		}
		states = append(states, e.Name+" "+state)
	}
	return states, nil
}

// stateOf is one restored session's state, as verify-restore prints it after the name.
func (a *App) stateOf(ctx context.Context, e restore.Entry, view hostView) (string, error) {
	live := view.live[e.Name]
	container, pane := "-", ""
	if live {
		var err error
		if pane, err = a.Tmux.CapturePane(ctx, e.Name, false); err != nil {
			a.failErr(err)
			return "", errReported
		}
		container = view.containerState(e.Name)
	}
	at, err := e.AtSeconds()
	if err != nil {
		a.failErr(err)
		return "", errReported
	}
	took, err := a.tookInput(e, at)
	if err != nil {
		return "", err
	}
	going := restore.GoingState(e.Going, at, e.Detail, a.Now().Unix(), int64(a.cfg.StartWindow.Seconds()), took)
	return restore.Judge(e.Prefix, live, pane, container, going).String(), nil
}

// runningContainers is the names of the running CCY containers of each engine, engines sorted.
func (a *App) runningContainers(ctx context.Context, engines []string) (map[string]bool, error) {
	slices.Sort(engines)
	running, err := sessprobe.RunningContainerNames(ctx, a.Runner, engines)
	if err != nil {
		a.fail(err.Error())
		return nil, errReported
	}
	return running, nil
}

// tookInput asks the transcript whether a session set going has taken what was typed. It is
// only asked of a session whose entry says something was typed.
func (a *App) tookInput(e restore.Entry, at int64) (restore.Took, error) {
	switch e.Going {
	case restore.GoingCompacting, restore.GoingCompact, restore.GoingContinue:
	case restore.GoingNone, restore.GoingPending, restore.GoingUntouched, restore.GoingKeep:
		return restore.TookNA, nil
	}
	took, err := transcript.TookInputSince(e.Transcript, at)
	if err != nil {
		a.failErr(err)
		return restore.TookNA, errReported
	}
	if took {
		return restore.TookYes, nil
	}
	return restore.TookNo, nil
}
