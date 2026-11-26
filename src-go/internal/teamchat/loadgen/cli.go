package loadgen

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Usage is the command's help.
const Usage = `usage: teamchat-loadgen setup --homeserver URL --server-name NAME --registration-key-file FILE --agents N --dir DIR
       teamchat-loadgen run --dir DIR [--pings N] [--questions N] [--notices N] [--reports N]
                            [--receipt-events N] [--receipts N] [--presence N] [--big N]
                            [--big-bytes N] [--for DURATION]
       teamchat-loadgen run --dir DIR --replay FILE [--speed N] [--skip-unmade] [--big-bytes N]

A load generator for a test team on a LAB homeserver (Plan 00268 Task 2.1). setup makes the
team once on a fresh server named under .test, listening on a loopback IP; run sends traffic
into it. Each pattern's N is units an hour (0 is off); --replay takes the JSON of
'teamchat-admin team traffic --json'. stdout is the tally as JSON; progress goes to stderr.
`

// Env is what the command runs with.
type Env struct {
	Stdout, Stderr io.Writer
	Clock          Clock
}

// usageError is a command line the command cannot take.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return &usageError{msg: fmt.Sprintf(format, args...)} }

// Main runs the command line args.
func Main(ctx context.Context, args []string, env Env) term.ExitCode {
	code, err := dispatch(ctx, args, env)
	if err == nil {
		return code
	}
	if ue, ok := errors.AsType[*usageError](err); ok {
		if _, werr := fmt.Fprintf(env.Stderr, "teamchat-loadgen: %s\n\n%s", ue.msg, Usage); werr != nil {
			return term.ExitFailure
		}
		return term.ExitUsage
	}
	if _, werr := fmt.Fprintf(env.Stderr, "teamchat-loadgen: %v\n", err); werr != nil {
		return term.ExitFailure
	}
	return code
}

func dispatch(ctx context.Context, args []string, env Env) (term.ExitCode, error) {
	if len(args) == 0 {
		return term.ExitUsage, usagef("name a verb")
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, err := io.WriteString(env.Stdout, Usage)
		return term.ExitOK, err
	case "setup":
		if err := setupVerb(ctx, args[1:], env); err != nil {
			return term.ExitFailure, err
		}
		return term.ExitOK, nil
	case "run":
		return runVerb(ctx, args[1:], env)
	}
	return term.ExitUsage, usagef("no verb %q", args[0])
}

func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usagef("%v", err)
	}
	if fs.NArg() > 0 {
		return usagef("an argument this verb does not take: %s", strings.Join(fs.Args(), " "))
	}
	return nil
}

func setupVerb(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	base := fs.String("homeserver", "", "")
	name := fs.String("server-name", "", "")
	keyFile := fs.String("registration-key-file", "", "")
	agents := fs.Int("agents", 0, "")
	dir := fs.String("dir", "", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	for flagName, v := range map[string]string{"--registration-key-file": *keyFile, "--dir": *dir} {
		if v == "" {
			return usagef("no %s", flagName)
		}
	}
	if err := CheckTarget(*base, *name); err != nil {
		return err
	}
	key, err := os.ReadFile(filepath.Clean(*keyFile))
	if err != nil {
		return fmt.Errorf("the registration key: %w", err)
	}
	team, err := Setup(ctx, SetupOptions{Base: *base, ServerName: *name, RegKey: strings.TrimSpace(string(key)), Agents: *agents, Clock: env.Clock})
	if err != nil {
		return err
	}
	if err := WriteTeam(*dir, team); err != nil {
		return err
	}
	file := filepath.Join(*dir, TeamFile)
	if _, err := fmt.Fprintf(env.Stderr, "teamchat-loadgen: team %s set up on %s with %d agents; sign Lightning in as %s, the password in %s\n",
		team.Team, team.Homeserver, len(team.Agents), team.Human.UserID, file); err != nil {
		return err
	}
	_, err = fmt.Fprintln(env.Stdout, file)
	return err
}

// runFlags are run's command line.
type runFlags struct {
	dir, replay      string
	rates            map[string]*int
	length           time.Duration
	speed, bigBytes  int
	skipUnmade       bool
	speedSet, forSet bool
}

func parseRun(args []string) (runFlags, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	f := runFlags{rates: map[string]*int{}}
	fs.StringVar(&f.dir, "dir", "", "")
	fs.StringVar(&f.replay, "replay", "", "")
	fs.DurationVar(&f.length, "for", time.Hour, "")
	fs.IntVar(&f.speed, "speed", 1, "")
	fs.IntVar(&f.bigBytes, "big-bytes", 0, "")
	fs.BoolVar(&f.skipUnmade, "skip-unmade", false, "")
	for _, p := range Patterns {
		f.rates[p] = fs.Int(p, 0, "")
	}
	if err := parse(fs, args); err != nil {
		return runFlags{}, err
	}
	fs.Visit(func(fl *flag.Flag) {
		f.speedSet = f.speedSet || fl.Name == "speed" || fl.Name == "skip-unmade"
		f.forSet = f.forSet || fl.Name == "for"
	})
	return f, nil
}

func (f runFlags) schedule(warn io.Writer) (Schedule, error) {
	perHour := map[string]int{}
	for p, n := range f.rates {
		if *n != 0 {
			perHour[p] = *n
		}
	}
	switch {
	case f.replay != "" && (len(perHour) > 0 || f.forSet):
		return nil, usagef("give patterns or --replay, not both")
	case f.replay == "" && f.speedSet:
		return nil, usagef("--speed and --skip-unmade go with --replay")
	case f.replay == "":
		return Rates(perHour, f.length)
	}
	data, err := os.ReadFile(filepath.Clean(f.replay))
	if err != nil {
		return nil, fmt.Errorf("the replay: %w", err)
	}
	_, counts, err := traffic.FromJSON(data)
	if err != nil {
		return nil, err
	}
	return Replay(counts, f.speed, f.skipUnmade, warn)
}

func runVerb(ctx context.Context, args []string, env Env) (term.ExitCode, error) {
	f, err := parseRun(args)
	if err != nil {
		return term.ExitUsage, err
	}
	if f.dir == "" {
		return term.ExitUsage, usagef("no --dir")
	}
	s, err := f.schedule(env.Stderr)
	if err != nil {
		return term.ExitFailure, err
	}
	team, err := ReadTeam(f.dir)
	if err != nil {
		return term.ExitFailure, err
	}
	g, err := NewGenerator(ctx, team, env.Clock, Options{BigBytes: f.bigBytes})
	if err != nil {
		return term.ExitFailure, err
	}
	defer g.Close()
	runErr := Run(ctx, g, s, env.Clock)
	data, err := json.Marshal(g.Tally())
	if err != nil {
		return term.ExitFailure, errors.Join(runErr, err)
	}
	if _, err := fmt.Fprintf(env.Stdout, "%s\n", data); err != nil {
		return term.ExitFailure, errors.Join(runErr, err)
	}
	switch {
	case runErr == nil:
		return term.ExitOK, nil
	case errors.Is(runErr, context.Canceled):
		return term.ExitCancelled, nil
	}
	return term.ExitFailure, runErr
}
