// Package rules holds this module's project rules (Defence Before Fix): go/analysis analyzers
// for the defect classes the ccy-teams and ccy-sessions ports actually hit. Each is proven on
// a fixture reproducing the defect (testdata) and run over src-go by go-qa (scripts/qa-go-qa.bash).
// CLAUDE/GoProjectRules.md is the catalogue.
//
// The tables below are the rules' data: which package owns which seam, and the narrow,
// reasoned exceptions. Nothing is suppressed in the code under check.
package rules

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// execOwners are the only packages that may start a process: every external command goes
// through the runner of one of them so a fake sees it. src-go has internal/execx; the nested
// qa module (a separate module, which cannot import src-go's internal packages) has its own
// seam, qa/internal/proc.
var execOwners = []string{execxPkg, "qa/internal/proc"}

// execOwnerNames is the owners as a message says them.
var execOwnerNames = strings.Join(execOwners, " or ")

// homeOwners may abbreviate the home directory: the term package, and the rules package, whose
// own source has to name the "~" it forbids everywhere else.
var homeOwners = []string{"internal/term", rulesPkg}

// rulesPkg is this package, whose own source names what several rules forbid.
const rulesPkg = "qa/rules"

const (
	sessprobePkg = "internal/sessprobe"
	podfreezePkg = "internal/podfreeze"
	lxcfreezePkg = "internal/lxcfreeze"
	keeperPkg    = "internal/teamchat/member/keeper"
	lxccmdPkg    = "internal/teamchat/host/lxc/lxccmd"
	sudoPkg      = "internal/sudo"
	agentbusPkg  = "internal/agentbus"
	hostPkg      = "internal/teams/host"
	opsPkg       = "internal/teams/ops"
	ftpcameraPkg = "internal/ftpcamera"
	execxPkg     = "internal/execx"
	wsitokenPkg  = "internal/wsitoken"
	sttPkg       = "internal/stt"
	lxcaddPkg    = "internal/teams/lxcadd"
	reclaimPkg   = "internal/reclaim"
	nordPkg      = "internal/nord"
	netpolicyPkg = "internal/netpolicy"
	systemdPkg   = "internal/systemd"
)

// The functions and the reason the realtime-bound tables repeat.
const (
	newFn             = "New"
	newEnvFn          = "NewEnv"
	runToolFn         = "gate.runTool"
	constantToolLimit = "the limit on a real tool; every caller passes a package constant and its tests fake the process"
)

// commandOwners maps a program to the packages that own every use of it, so its flags and
// target forms are decided once (the gum and fzf prompts, the sudo and tmux argument forms).
// A program with two owners is used for two different jobs: podman is read for a session's
// container by sessprobe, driven (pause, unpause, networks) by podfreeze, and pruned (with
// docker) by reclaim, the disk-space menu; netpolicy creates, holds the network policy of and
// starts a ccy session's container (and runs one under docker unprotected); nmcli is run inside
// a container by lxcfreeze, for the hotspot by ftpcamera and for its VPN profiles by nord.
// systemctl is the desktop's units through sudo in systemd and `clean --what=state` of a
// teamchat homeserver as root in teamchat/units (whose other unit calls are D-Bus). lxc-ls,
// lxc-info and lxc-attach are the freeze tool's through sudo in lxcfreeze, and teamchat-admin's
// as root in teamchat/host/lxc/lxccmd, which may not link lxcfreeze's TUI or sudo. A program absent
// from the table is unowned: any package may run it.
var commandOwners = map[string][]string{
	"sudo":         {sudoPkg},
	"tmux":         {"internal/tmuxccy"},
	"gum":          {"internal/ui"},
	"fzf":          {"internal/ui"},
	"systemctl":    {systemdPkg, "internal/teamchat/units"},
	"busctl":       {systemdPkg, "internal/wsimedia"},
	"git":          {"internal/gitx"},
	"ps":           {sessprobePkg},
	"podman":       {sessprobePkg, podfreezePkg, reclaimPkg, "internal/ccc", netpolicyPkg},
	"docker":       {sessprobePkg, reclaimPkg, netpolicyPkg},
	"lxc-ls":       {lxcfreezePkg, lxccmdPkg},
	"lxc-info":     {lxcfreezePkg, lxccmdPkg},
	"lxc-freeze":   {lxcfreezePkg},
	"lxc-unfreeze": {lxcfreezePkg},
	"lxc-attach":   {lxcfreezePkg, lxccmdPkg},
	"nmcli":        {lxcfreezePkg, ftpcameraPkg, nordPkg},
}

// programRunner is a function or method that runs the program named by its arguments from
// argIndex on (the program, then its arguments) without the caller building an execx.Cmd, so
// the call is judged as the Cmd would be. recv is the receiver type name, "" for a package
// function.
type programRunner struct {
	pkg      string
	recv     string
	method   string
	argIndex int
}

// sudoType is the receiver type of the sudo seam.
const sudoType = "Sudo"

// programRunners are the entry points that run a program from their arguments. sudo's three
// run any program as root; lxcfreeze's two helpers are the same shape one level up, so their
// callers are judged too.
var programRunners = []programRunner{
	{pkg: sudoPkg, recv: sudoType, method: "Capture", argIndex: 1},
	{pkg: sudoPkg, recv: sudoType, method: "AsRoot", argIndex: 1},
	{pkg: sudoPkg, recv: sudoType, method: "Run", argIndex: 1},
	{pkg: lxcfreezePkg, recv: "Engine", method: "runAct", argIndex: 1},
	{pkg: lxcfreezePkg, recv: "Engine", method: "probe", argIndex: 1},
	{pkg: ftpcameraPkg, recv: "cli", method: "asRoot", argIndex: 1},
	{pkg: ftpcameraPkg, recv: "cli", method: "rootCapture", argIndex: 1},
}

// shells are interpreters that run a string given with -c; handing one to the runner is the
// sh -c route round the argv the runner and its fake exist to show.
var shells = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true}

// allowance excuses one function of one package from a rule. It is data, with the reason,
// never a comment in the code. pkg is a package path suffix, as for an owner.
type allowance struct {
	pkg    string
	fn     string // the enclosing function; "" is a package-level declaration
	reason string
}

// dynamicNames are the functions that may build an execx.Cmd whose Name is not a constant,
// because the program is chosen at run time. Every other Name is a constant, so the owner
// table can judge it. A dynamic Name is not judged against the owner table, so each row says
// what it can be. fn is "func" or "Type.method".
var dynamicNames = []allowance{
	{
		pkg:    sessprobePkg,
		fn:     "enginePS",
		reason: "the container engine (podman or docker) the caller found; sessprobe owns both",
	},
	{
		pkg:    sessprobePkg,
		fn:     "Probe.inspectRunning",
		reason: "the container engine (podman or docker) found in a session's process tree; sessprobe owns both",
	},
	{
		pkg:    "internal/sessions",
		fn:     "route.run",
		reason: "a project's daemon CLI path, or the container engine that execs it inside the project's container",
	},
	{
		pkg:    "internal/engine",
		fn:     "Engine.cmd",
		reason: "the ccy launcher's container engine, podman or docker: an Engine's kind is unexported and set only by engine.New, which refuses any other name",
	},
	{
		pkg:    "internal/ccylaunch",
		fn:     "Front.handOver",
		reason: "the bash launcher, claude-yolo.bash beside the Go front's own executable: the strangler's hand-over (Plan 00191 Task 4.5)",
	},
	{
		pkg:    "internal/ccystart",
		fn:     "Start",
		reason: "the ccy session's own command as the container was started with it, behind the bus claim or teamchat run and the supervisor wrapper: the entrypoint's final exec (Plan 00209 Task 4.2)",
	},
	{
		pkg:    "internal/teams/teamchatlaunch",
		fn:     "Verbs.busDisconnected",
		reason: "the host pingbus the ccy launcher names (its CCY_PINGBUS_COMMAND, the agent-bus kit's or CCY_TEAMS_PINGBUS), exec'd with the launcher's own arguments for an agent-bus session's exit (Plan 00209 Task 4.2)",
	},
	{
		pkg:    hostPkg,
		fn:     "launcherVia",
		reason: "the ccy launcher path the process is replaced with, given by the environment",
	},
	{
		pkg:    opsPkg,
		fn:     "Ops.runPlay",
		reason: "the agent-bus play, run by its own shebang; its path is configuration",
	},
	{
		pkg:    opsPkg,
		fn:     "Ops.elementPlay",
		reason: "the Element play, run by its own shebang; its path is configuration",
	},
	{
		pkg:    "internal/teamchat/admin",
		fn:     "ExecChild.Ask",
		reason: "teamchat-admin itself, run as the teamchat-ctl child: the running binary resolved from /proc/self/exe (SPEC-n2f CS2)",
	},
	{
		pkg:    "internal/teamchat/matrix/tuwunelhs",
		fn:     "StartWith",
		reason: "the Tuwunel homeserver binary the environment names (BinEnv), run for the real-homeserver tests; never an owned program",
	},
	{
		pkg:    "internal/teamchat/admin",
		fn:     "UnitChild.Ask",
		reason: "/usr/bin/systemd-run of the restore's secure unit (it refuses any other argv[0]), argv from restore.SecureArgv (SPEC-n2g RU2); the program inside is teamchat-admin itself, a run-time path",
	},
	{
		pkg:    "internal/teamchat/restore",
		fn:     "unitCommand",
		reason: "/usr/bin/systemd-run of the restore's staging or secure unit, argv from StagingArgv or SecureArgv (SPEC-n2g RU1, RU2); the program inside is the Tuwunel units.json names or teamchat-admin itself, both run-time paths",
	},
	{
		pkg:    "internal/wsi",
		fn:     "run.sibling",
		reason: "wsi-stop-grace, wsi-setting or wsi-resolve-model, run from the directory the wsi binary is in",
	},
	{
		pkg:    "internal/open",
		fn:     "run.runHandler",
		reason: "the terminal handler the person chose for a file (a viewer, pager, editor or player from open's table, or $VISUAL/$EDITOR) and its pager; never an owned program",
	},
	{
		pkg:    "internal/wsipost",
		fn:     "App.postProcess",
		reason: "wsi-claude-process, run from the directory the wsi-postprocess binary is in",
	},
	{
		pkg:    wsitokenPkg,
		fn:     "App.setting",
		reason: "wsi-setting, run from the directory the wsi-claude-token binary is in",
	},
	{
		pkg:    wsitokenPkg,
		fn:     "App.readUsage",
		reason: "ccy's launcher, whose installed path is configuration (wsitoken.DefaultCCY in production)",
	},
	{
		pkg:    sttPkg,
		fn:     "App.language",
		reason: "wsi-setting, run from the directory the stt binary is in",
	},
	{
		pkg:    sttPkg,
		fn:     "App.ensureServer",
		reason: "wsi-stream --ensure-server, run from the directory the stt binary is in",
	},
	{
		pkg:    lxcfreezePkg,
		fn:     "Engine.runAct",
		reason: "runs the lxc command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    lxcfreezePkg,
		fn:     "Engine.probe",
		reason: "runs the read-only command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    lxccmdPkg,
		fn:     "LXC.Guest",
		reason: "the host-written kit binary <kit>/bin/teamchat, its digest path resolved once by the host and never current (C 3.9.1)",
	},
	{
		pkg:    agentbusPkg,
		fn:     "Client.rootRun",
		reason: "runs the root tool its caller names; the callers pass the sudo.AgentBus and sudo.InstallTool paths",
	},
	{
		pkg:    agentbusPkg,
		fn:     "Client.rootShow",
		reason: "streams the root tool its caller names; the callers pass the sudo.AgentBus and sudo.InstallTool paths",
	},
	{
		pkg:    ftpcameraPkg,
		fn:     "cli.asRoot",
		reason: "runs the root command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    ftpcameraPkg,
		fn:     "cli.rootCapture",
		reason: "runs the root command its caller names; every caller is itself judged as a program runner",
	},
	{
		pkg:    "internal/teams/hot",
		fn:     "Hot.pingbus",
		reason: "the host's pingbus from the agent team bus kit, whose path is configuration (CCY_TEAMS_PINGBUS)",
	},
	{
		pkg:    ftpcameraPkg,
		fn:     "cli.execVault",
		reason: "the checkout's vault.bash, whose path is REPO_PATH in /etc/ftp-camera/config",
	},
	{
		pkg:    "internal/teamchat/reexec",
		fn:     "command",
		reason: "the test binary itself, re-run as a second process (SPEC-n3 gap 8)",
	},
	{
		pkg:    "internal/teamchat/failpoint",
		fn:     "runSelf",
		reason: "the busfailpoints test binary itself, re-run so an armed point can SIGKILL it (SPEC-n3 gap 9)",
	},
	{
		pkg:    "internal/teamchat/member/guest",
		fn:     "Checker.ghLoggedIn",
		reason: "gh (unowned) at the path guest check found on the session unit's PATH, never the attach's own (SPEC-n3 GU2)",
	},
	{
		pkg:    keeperPkg,
		fn:     "linkCmd",
		reason: "the kit's own teamchat, <kit>/bin/teamchat link, for the kit the keeper resolved (SPEC-n3 RN1)",
	},
	{
		pkg:    keeperPkg,
		fn:     "keeper.kitVersion",
		reason: "the kit's own teamchat asked its --version, for the kit --kit or the current link names (SPEC-n3 RN1)",
	},
	{
		pkg:    keeperPkg,
		fn:     "keeper.resolveKit",
		reason: "the kit's own teamchat re-executed as run --kit when it is another version than this binary (SPEC-n3 RN1)",
	},
	{
		pkg:    keeperPkg,
		fn:     "keeper.startCmd",
		reason: "CMD, the session's command the launcher named after --, Claude Code or a plain command (B 2.4.1 step 7)",
	},
}

// shellStrings are the functions that may hand a shell a string with -c, because the program's
// own specification is a command line a user wrote for a shell to run. Every other shell -c is
// the route round the argv the runner shows. fn is "func" or "Type.method".
var shellStrings = []allowance{
	{
		pkg:    "internal/pingbus/statusline",
		fn:     "runInner",
		reason: "the statusLine command of the session's settings, which Claude Code itself runs as /bin/sh -c (Plan 00213 SPEC-status-line.md S9); the status line runs it the same way",
	},
	{
		pkg:    "internal/teamchat/member/statusline",
		fn:     "deps.runInner",
		reason: "the session's own statusLine command, which Claude Code runs as /bin/sh -c; teamchat status-line wraps it the same way (Plan 00209 B 2.9, SPEC-n3 SL1)",
	},
}

// bindingSubjects are not judged by project_hidden_bindings: the rules package and the
// shell gate of the same rule name the fzf flags as the text they look for and build no fzf
// argv (neither starts fzf).
var bindingSubjects = []string{rulesPkg, "qa/hiddenbindings"}

// expectBuilders are the only functions that may build fzf's --expect: the keys fzf hands
// back for the caller to act on. Each takes its keys from ui.Action values, whose Label ui
// shows as a row or a menu line, so no key is the only way to its action.
var expectBuilders = []allowance{
	{
		pkg:    uiPkg,
		fn:     "expectArg",
		reason: "the keys are the Key fields of the screen's Actions and row Menus, each listed by ui with its Label",
	},
}

// waiterCountAllowances are the test functions whose claim is that nothing at all is armed on
// their fake clock, so a count of every waiter is the claim itself (project_fakeclock_count).
// A row excuses an assertion of the count, never a wait on it.
var waiterCountAllowances = []allowance{
	{
		pkg:    "internal/teamchat/member/client",
		fn:     "world.noLink",
		reason: "LN3: with no link the client answers at once and never waits, so any armed timer is a wait it must not make",
	},
	{
		pkg:    "internal/teamchat/member/client",
		fn:     "TestASendNeverRetries",
		reason: "a send never reconnects or waits, so any armed timer is a wait it must not make",
	},
	{
		pkg:    "internal/teamchat/member/inbox",
		fn:     "TestOneDeliveryRecordsTheAuthFrameThenTheContent",
		reason: "the inbox client is the only code on its clock, and a finished delivery leaves no phase timer of any kind armed",
	},
	{
		pkg:    "internal/teamchat/member/faketranscript",
		fn:     "TestRecordNeverWritesNothing",
		reason: "a record whose delay is Never arms nothing at all on the transcript's clock",
	},
}

// agentBranches are the declarations outside package agent that may name or compare a launch
// value (project_agent_branch): its one list, the two checks of a value read from a file or a
// frame, and cli's names and spelling of it. fn "" is a package-level declaration.
var agentBranches = []allowance{
	{pkg: teamchatStatePkg, fn: "", reason: "the one list of launch values"},
	{pkg: teamchatStatePkg, fn: "SessionPID.check", reason: "session.pid's value check"},
	{pkg: teamchatWirePkg, fn: "Launch.check", reason: "the launch frame's value check"},
	{pkg: teamchatCLIPkg, fn: "", reason: "cli's names for the launch values"},
	{pkg: teamchatCLIPkg, fn: "ParseLaunch", reason: "--resume-recorded refused on plain"},
	{pkg: teamchatCLIPkg, fn: "Launch.fromFlags", reason: "the spelling"},
}

// clockAllowance excuses one test function from the fixed-clock rule.
type clockAllowance = allowance

// No test reads the wall clock: a real sleep is shown by a timer that fires first.
var clockAllowances = []clockAllowance{}

// teamchatClockPkg is teamchat's clock, the one home of teamchat's real timers: every other
// teamchat package's real Sleep, AfterFunc and Hold is its System's (the repository owner's
// decision on pull request 124).
const teamchatClockPkg = "internal/teamchat/clock"

// realClocks are the real side of a clock seam: each is handed its duration by a caller whose
// tests replace the seam whole, so no test shortens the real one. fn is "func" or "Type.method".
var realClocks = []allowance{
	{pkg: "qa/internal/qalock", fn: "RealClock.Sleep", reason: "the real side of qalock.Clock, the QA lock's one-second poll; its tests replace the clock whole, and the one that keeps RealClock asserts only that a cancelled context ends it"},
	{pkg: "internal/tmuxccy", fn: "Tmux.pause", reason: "the real side of Tmux.Sleep, the take-over's wait between client checks; the one test that keeps it runs in a synctest bubble"},
	{pkg: "internal/ccylaunch", fn: "Front.pause", reason: "the real side of Front.Pause, the build lock's poll; its tests replace Pause"},
	{pkg: sessprobePkg, fn: "Probe.sleep", reason: "the real side of Probe.Sleep, the CPU sample; the one test that keeps it samples fixed /proc readings, so nothing races the sleep"},
	{pkg: "internal/playlock", fn: "sleep", reason: "the real side of Waiter.Sleep, the wait between lock tries; its tests replace Sleep"},
	{pkg: sttPkg, fn: "sleep", reason: "the real side of Env.Sleep; its tests replace Sleep"},
	{pkg: ftpcameraPkg, fn: "sleepContext", reason: "the real side of Env.Sleep; its tests replace Sleep, and NewEnv's test sleeps 0"},
	{pkg: "internal/vmtest", fn: "sleepContext", reason: "the real side of Env.Sleep; its tests replace Sleep"},
	{pkg: "internal/vmtest", fn: "newCLI", reason: "the real side of cli.bound, a remote command's deadline (timeout(1) in the bash); its tests replace bound whole (expiredBound)"},
	{pkg: "internal/wsi", fn: newEnvFn, reason: "the real side of Env.Sleep and Env.After; its tests replace both"},
	{pkg: "internal/wsimedia", fn: newEnvFn, reason: "the real side of Env.After, the hold's poll; its tests replace After, and NewEnv's test asks for 0"},
	{pkg: "internal/rclonetail", fn: newEnvFn, reason: "the real side of Env.After, the wait between ticks; its tests replace After, and NewEnv's test never waits"},
	{pkg: "internal/teams/restart", fn: newFn, reason: "the real side of Restart.Sleep; its tests replace Sleep, and the picker's never reach a wait"},
	{pkg: "internal/teams/shutdown", fn: newFn, reason: "the real side of Shutdown.Sleep; its tests replace Sleep, and the picker's never reach a wait"},
	{pkg: "internal/teams/winddown", fn: newFn, reason: "the real side of WindDown.Sleep; its tests replace Sleep, and the picker's never reach a wait"},
	{pkg: "internal/sessclose", fn: newFn, reason: "the real side of Closer.Sleep; its tests and every caller's replace Sleep (shutdown, ccy-sessions and the ccy front set their own clock), and the picker's never reach a wait"},
	{pkg: "internal/teams/hot", fn: newFn, reason: "the real side of Hot.Sleep; its tests replace Sleep"},
	{pkg: teamchatClockPkg, fn: "system.Sleep", reason: "the real side of clock.Clock's Sleep, teamchat's one real wait (lock, teamlock, admin/wire and matrix hand theirs to it); the tests that wait on a bound replace the clock whole with clock.Fake, and the ones that keep System assert only that a sleep ends and that a cancelled context ends it"},
	{pkg: teamchatClockPkg, fn: "system.AfterFunc", reason: "the real side of clock.Clock's AfterFunc, the timer of every teamchat time limit (matrix.SystemTimers is it); the tests that reach a limit replace the clock or matrix's Timers whole, and the ones that keep System assert only that a timer fires and that stop disarms one"},
	{pkg: teamchatClockPkg, fn: "system.Hold", reason: "the real side of clock.Clock's Hold, guestroot's call bound and fakehs's held-sync timeout; the tests that reach a hold's end replace it with clock.Fake's, and the ones that keep System assert only that a hold fires and that stop ends it, or hold a sync that an event ends first"},
}

// processBounds bound a real child process or real I/O. No test shortens one so that it races
// a step: their tests fake the process and report the timeout, leave the production constant,
// or shorten it only where its passing is the outcome asserted. fn is "func" or "Type.method".
var processBounds = []allowance{
	{pkg: execxPkg, fn: "OS.Run", reason: "Grace as exec.Cmd.WaitDelay, the wait for a finished run's pipes; the one test that shortens it has a child hold the pipe for 30 s, so the wait running out is the outcome asserted"},
	{pkg: execxPkg, fn: "OS.session", reason: "the grace for a killed run's pipes to drain; the tests that shorten Grace assert only what holds whichever of the drain and the grace ends first"},
	{pkg: "internal/restore", fn: "acquire", reason: "the wait for a flock another run holds; the tests that shorten it hold the lock throughout, so the wait running out is the outcome asserted"},
	{pkg: "internal/sessions", fn: "pauseFor", reason: "the pause between redraws, ended early by a real signal; its tests hand it a signal already waiting or none"},
	{pkg: wsitokenPkg, fn: "App.readUsage", reason: "the limit on the real ccy --token-usage run; the test that shortens it to 1 ns answers after 1 ms, so the limit has always passed"},
	{pkg: "internal/teams/roles", fn: "entries.isMember", reason: "the limit on a real read of a container's checkout, the constant New sets; no test changes it"},
	{pkg: lxcaddPkg, fn: "Adder.sessions", reason: "the limit on a real read of the container, the constant New sets; no test changes it"},
	{pkg: lxcaddPkg, fn: "Adder.findCheckouts", reason: "the limit on the real home search; the test that shortens it to 1 ns hangs its fake until the context ends, so the limit passing is the outcome asserted"},
	{pkg: lxcaddPkg, fn: "Adder.listDir", reason: "the limit on a real directory listing; the test that shortens it to 1 ns hangs its fake until the context ends, so the limit passing is the outcome asserted"},
	{pkg: "qa/all", fn: "session.exec", reason: "QA_ALL_TIMEOUT on a real gate; its tests fake the process and report the timeout"},
	{pkg: "qa/changed", fn: "session.timed", reason: "QA_CHANGED_TIMEOUT on a real check; the test that shortens it runs in a synctest bubble"},
	{pkg: "qa/ansiblesyntax", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/bash", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/ghscopes", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/js", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/patterns", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/python", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/readywait", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/sttrules", fn: runToolFn, reason: constantToolLimit},
	{pkg: "qa/helpertests", fn: "suite", reason: "Env.Timeout on the real unittest runner; its tests fake the runner, which ignores the deadline"},
	{pkg: "qa/hookhandlers", fn: "gate.runTests", reason: "Env.Timeout on the real pytest run; its tests fake the runner, which ignores the deadline"},
	{pkg: "qa/plantests", fn: "execute", reason: "Env.Timeout on each directory's real test run; its tests fake the runner, which ignores the deadline"},
	{pkg: "qa/toolchain", fn: "assertion.read", reason: "Env.Timeout on a real tool's version run; its tests fake the runner, which ignores the deadline"},
	{pkg: "qa/vmtestmanifest", fn: "gate.validate", reason: "Env.Timeout on the real Python validator; its tests fake it, and the real one runs under the minute default"},
}

// identityFoldAllowances are the functions project_identity_fold does not judge: each reads a
// GitHub name back from a file the program wrote in the folded form.
var identityFoldAllowances = []allowance{
	{pkg: "internal/teamchat/member/state", fn: "validRef", reason: "validates the member's own state file, whose references the sender wrote after proto.LowercaseRefRepo folded them; a person never writes it"},
}

// realTimeAllowances are the functions project_realtime_bound does not judge.
func realTimeAllowances() []allowance {
	return slices.Concat(realClocks, processBounds)
}

// noResult is what every rule hands the analysis framework: the rules only report.
type noResult struct{}

var noResultType = reflect.TypeFor[noResult]()

// perPass adapts a reporting function to an analyzer's Run.
func perPass(run func(*analysis.Pass)) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		run(pass)
		return noResult{}, nil
	}
}

// All is every rule, in the order the driver registers them.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{SeamExec, SeamCommands, TestClock, HomeAbbrev, TmuxTargets, WaitDelay, HiddenBindings, PackageTests, RefusalKind, RealTimeBound, EscJoined, DurationOverflow, CommandFailureDropped, SelfErasingClipboard, LivenessWord, LifecycleTurn, KeeperLogBarrier, FakeClockCount, GoroutineEnd, RawConnProbe, LeakcheckOnce, AgentBranch, TransportHome, HostnameRead, ProblemErrorText, TestChown, ListingOrder, TestSpin, PollPure, FakeRestart, WalkOpen, ZombieExit, ContainerPidsLimit, RowKind, ChildExit, SessionKind, SeatMemberType, RoomDefault, SharedDue, CoordinatorAbsence, IdentityFold, SemgrepTestRunner, SelfChildStderr, UnitNoInstall, SecretScrollback, InboxSocketDir, SubmitUnchecked}
}

// ownedBy is whether the package path (the test variants of it included) is owner, matched
// as a path suffix on a whole element so the fixtures and the real module share the table.
func ownedBy(pkgPath, owner string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "_test")
	return path == owner || strings.HasSuffix(path, "/"+owner)
}

// ownedByAny is whether the package path is any of the owners.
func ownedByAny(pkgPath string, owners []string) bool {
	return slices.ContainsFunc(owners, func(o string) bool { return ownedBy(pkgPath, o) })
}

// funcName is the name an allowance row uses for a declaration: "func" or "Type.method",
// and "" for a package-level declaration that is not a function.
func funcName(decl ast.Decl) string {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok {
		return ""
	}
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if star, isStar := recv.(*ast.StarExpr); isStar {
		recv = star.X
	}
	if idx, isIdx := recv.(*ast.IndexExpr); isIdx {
		recv = idx.X
	}
	if id, isIdent := recv.(*ast.Ident); isIdent {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// isTestFile is whether the file is a _test.go file.
func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(filepath.Base(pass.Fset.Position(f.Pos()).Filename), "_test.go")
}

// calledFunc is the function a call expression calls, or nil for a conversion, a builtin or
// a call through a value.
func calledFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return nil
	}
	f, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return f
}

// isPkgFunc is whether f is the package-level function pkgPath.name (not a method).
func isPkgFunc(f *types.Func, pkgPath, name string) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != pkgPath || f.Name() != name {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Recv() == nil
}
