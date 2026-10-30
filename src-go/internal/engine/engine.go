// Package engine is the ccy launcher's container engine: which one a launch uses, the few
// questions it asks of it, and the verdicts on its answers that decide whether a launch may go
// on (Plan 00191 Task 4.5, phase 1.1 of ASSESS-ccy-launcher.md). The verdicts are pure
// functions of the engine's text, so every answer, the unsafe ones included, is tested without
// an engine; the questions go through an execx.Runner.
//
// Intended behaviour, each held by a test (bash, where it differs, is noted):
//
//   - E1. The engine is podman unless CCY_CONTAINER_ENGINE names one; --engine VALUE wins over
//     the variable. An empty variable is unset. Names are exact: podman or docker.
//   - E2. A name that is not an engine is refused when the engine is chosen, whatever it came
//     from: an unknown --engine value is a usage error (exit 64), an unknown variable a failure
//     (exit 1). Bash refused an unknown variable only at token validation, after the banners,
//     and said "not found" for any name it could not run.
//   - E3. --engine with nothing after it, or with the separator after it, is a usage error
//     (exit 64). Bash ignored a trailing --engine and launched with the default.
//   - E4. A chosen engine whose program is not on PATH is a failure naming where the choice
//     came from.
//   - E5. The rootless verdict (RootlessVerdict) and the SELinux verdict (SELinuxVerdict) read
//     the engine's report: an answer that is not exactly one the engine gives is unknown, and
//     unknown never passes the rootless guard and always relabels.
//   - E6. Docker's SELinux report is its SecurityOptions list; text that is not such a list is
//     kept as it is, so an unreachable daemon reads as unknown and relabels. Bash read it as
//     "not labelling" and turned the relabel off.
//   - E7. An image inspect that fails with the engine's "no such image" wording is a missing
//     image; any other failure is an error, not a missing image. Bash read every failure as a
//     missing image and went on to build one.
//   - E8. The relabel preflight's pure parts: the uid map (ParseUIDMap: three numbers a line,
//     no range of no uids, at least one range; anything else refuses), which owners are
//     foreign (Maps, ForeignOwnerArgs), whether foreign entries block (RelabelBlocks), when
//     the check applies (NeedsRelabelCheck), and the look and fix commands shown to the
//     person, the fix being exactly the argv that runs (FixCommand quotes FixArgv, `+`
//     included). The uid map is read from stdout only.
//   - E9. An Engine is built only by New, which refuses any kind but podman or docker; the
//     zero Engine runs nothing. So the program an Engine runs is always one of the two.
//   - E10. ccy --help's two questions: a bare image inspect (Inspect), whose every refusal the
//     help reads as "not built yet" as the bash launcher did, since it only says how to build
//     the image (an engine that could not run is said with its error); and claude --help run in
//     the image (ClaudeHelp), its output merged.
//   - E11. A run in the image given a secret (RunWithSecret, ccy's token check): the secret by
//     name only (`-e NAME`, an execx.Secret, so a command whose argv holds it is refused), the
//     image never pulled (`--pull=never`), and a missing image (the engine's exit 125 with its
//     "no such image" wording) told apart from every other failure, the container's included.
//   - E12. The launch's network check and version probe: a run on a named network
//     (RunOnNetwork), `claude --version` in the image (ClaudeVersion, merged), and
//     ContainerFailed, which tells the container command's own non-zero exit from the
//     engine's (125, 126, 127, a signal, or no engine started), so a check can say which
//     failed.
//   - E13. A container stop (Stop) and a network removal (RemoveNetwork) that fail are the
//     engine's exit with its words; whether the object is still there is asked of the store
//     (StoreHas), never read from those words.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ErrNoImage is an image the engine says it does not have.
var ErrNoImage = errors.New("no such image")

// ErrUnknownKind is an Engine whose Kind is not one ccy drives.
var ErrUnknownKind = errors.New("not a container engine ccy drives")

// ErrNoUIDMap is a uid map asked of docker, which has no `unshare`.
var ErrNoUIDMap = errors.New("only podman reports a user-namespace uid map")

// ErrNoRunner is an Engine asked for with no runner to run it through.
var ErrNoRunner = errors.New("a container engine needs a runner")

// Engine runs the questions a launch asks of one container engine. Its fields are unexported
// so that New is the only way to build one: the program it runs is podman or docker, never a
// name from anywhere else. The zero Engine runs nothing (ErrUnknownKind).
type Engine struct {
	kind   Kind
	runner execx.Runner
}

// New is the engine of the given kind, run through r; any kind but Podman or Docker is
// ErrUnknownKind.
func New(kind Kind, r execx.Runner) (Engine, error) {
	if _, ok := parse(string(kind)); !ok {
		return Engine{}, fmt.Errorf("%q: %w", kind, ErrUnknownKind)
	}
	if r == nil {
		return Engine{}, ErrNoRunner
	}
	return Engine{kind: kind, runner: r}, nil
}

// Kind is the engine's program.
func (e Engine) Kind() Kind { return e.kind }

func lines(out []byte) []string {
	var got []string
	for line := range strings.Lines(string(out)) {
		if line = strings.TrimSpace(line); line != "" {
			got = append(got, line)
		}
	}
	return got
}

// missingImage is the wording podman ("image not known") and docker ("No such image", or
// "No such object" before 20.10) give an image they do not have.
func missingImage(stderr []byte) bool {
	text := strings.ToLower(string(stderr))
	return strings.Contains(text, "image not known") || strings.Contains(text, "no such image") ||
		strings.Contains(text, "no such object")
}

// ImageInspect is one field of an image, by an engine --format template, trimmed;
// ErrNoImage when the engine does not have it.
func (e Engine) ImageInspect(ctx context.Context, image, format string) (string, error) {
	res, err := e.run(ctx, "image", "inspect", "--format", format, image)
	if err != nil {
		if exit, ok := errors.AsType[*execx.ExitError](err); ok && exit != nil && missingImage(res.Stderr) {
			return "", fmt.Errorf("%s: %w", image, ErrNoImage)
		}
		return "", fmt.Errorf("inspect image %s: %w", image, err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// ImageExists reports whether the engine has the image.
func (e Engine) ImageExists(ctx context.Context, image string) (bool, error) {
	_, err := e.ImageInspect(ctx, image, "{{.Id}}")
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoImage):
		return false, nil
	default:
		return false, err
	}
}

// Inspect is a bare `image inspect IMAGE`, its output discarded: nil when the engine answered
// for the image. --help asks only this, and says how to build the image on any refusal, as the
// bash launcher did (E10); a launch asks ImageExists, which tells a missing image apart.
func (e Engine) Inspect(ctx context.Context, image string) error {
	if _, err := e.run(ctx, "image", "inspect", image); err != nil {
		return fmt.Errorf("inspect image %s: %w", image, err)
	}
	return nil
}

// ClaudeHelp is `claude --help` run in the image, stdout and stderr as one stream: on a failure
// the text is returned with the error, so the person sees why it would not run (E10).
func (e Engine) ClaudeHelp(ctx context.Context, image string) (string, error) {
	if err := e.known(); err != nil {
		return "", err
	}
	out, err := execx.Combined(ctx, e.runner, e.cmd("run", "--rm", "--entrypoint", "claude", image, "--help"))
	if err != nil {
		return out, fmt.Errorf("claude --help in %s: %w", image, err)
	}
	return out, nil
}

// engineFailed is the exit code podman and docker give a run they could not start, a missing
// image among them; the container's own exit codes are passed through as they are.
const engineFailed = 125

// RunWithSecret is `run --rm --pull=never -e NAME --entrypoint ENTRY IMAGE ARGS...`, its output
// captured (E11): the secret reaches the container by its name only, never in argv, and the
// image is never pulled, so an image the engine does not have is ErrNoImage. Any other failure
// is returned as it is: the engine's own (exit 125, or no engine started) or the container's.
func (e Engine) RunWithSecret(ctx context.Context, image string, secret execx.Secret, entrypoint string, args ...string) (execx.Result, error) {
	if err := e.known(); err != nil {
		return execx.Result{}, err
	}
	c := e.cmd(append([]string{"run", "--rm", "--pull=never", "-e", secret.Name(), "--entrypoint", entrypoint, image}, args...)...)
	c.Secrets = []execx.Secret{secret}
	res, err := e.runner.Run(ctx, c)
	if exit, ok := errors.AsType[*execx.ExitError](err); ok && exit != nil && exit.Code == engineFailed && missingImage(res.Stderr) {
		return res, fmt.Errorf("%s: %w", image, ErrNoImage)
	}
	return res, err
}

// RunOnNetwork is `run --rm --network NETWORK IMAGE ARGS...`, its output captured (E12): the
// image pulled when the engine lacks it, as a plain run does. ContainerFailed tells the
// container's own failure from the engine's.
func (e Engine) RunOnNetwork(ctx context.Context, network, image string, args ...string) (execx.Result, error) {
	return e.run(ctx, append([]string{"run", "--rm", "--network", network, image}, args...)...)
}

// ClaudeVersion is `claude --version` run in the image, stdout and stderr as one stream; on a
// failure the error carries that text (E12).
func (e Engine) ClaudeVersion(ctx context.Context, image string) (string, error) {
	if err := e.known(); err != nil {
		return "", err
	}
	out, err := execx.Combined(ctx, e.runner, e.cmd("run", "--rm", "--entrypoint", "claude", image, "--version"))
	if err != nil {
		return out, fmt.Errorf("claude --version in %s: %w", image, err)
	}
	return out, nil
}

// ContainerFailed reports whether err is a run's container command ending itself non-zero:
// not the engine failing to run it (125), the command not runnable (126) or not found (127)
// in the image, a signal, or an engine that did not start (E12).
func ContainerFailed(err error) bool {
	exit, ok := errors.AsType[*execx.ExitError](err)
	return ok && exit != nil && exit.Signal == 0 && exit.Code != 0 && exit.Code < engineFailed
}

// ContainerQuery is a `ps`: every container (All) or the running ones, narrowed by the
// engine's --filter expressions.
type ContainerQuery struct {
	All     bool
	Filters []string
}

// Containers is the names of the containers the query selects.
func (e Engine) Containers(ctx context.Context, q ContainerQuery) ([]string, error) {
	args := []string{"ps"}
	if q.All {
		args = append(args, "-a")
	}
	for _, f := range q.Filters {
		args = append(args, "--filter", f)
	}
	res, err := e.run(ctx, append(args, "--format", "{{.Names}}")...)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return lines(res.Stdout), nil
}

// Remove removes a container, running or not (`rm -f`).
func (e Engine) Remove(ctx context.Context, name string) error {
	if _, err := e.run(ctx, "rm", "-f", name); err != nil {
		return fmt.Errorf("remove container %s: %w", name, err)
	}
	return nil
}

// Stop is `stop NAME` (E13).
func (e Engine) Stop(ctx context.Context, name string) error {
	if _, err := e.run(ctx, "stop", name); err != nil {
		return fmt.Errorf("stop container %s: %w", name, err)
	}
	return nil
}

// RemoveNetwork is `network rm NAME` (E13).
func (e Engine) RemoveNetwork(ctx context.Context, name string) error {
	if _, err := e.run(ctx, "network", "rm", name); err != nil {
		return fmt.Errorf("remove network %s: %w", name, err)
	}
	return nil
}

// Networks is the names of the engine's networks.
func (e Engine) Networks(ctx context.Context) ([]string, error) {
	res, err := e.run(ctx, "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	return lines(res.Stdout), nil
}

// Report is the engine's answer to the rootless question with its verdict: the command as a
// person would type it, what it printed (stdout and stderr, as one stream) and its exit
// status, for the refusal to show.
type Report struct {
	Posture  Posture
	Command  string
	Output   string
	ExitCode int
}

// Rootless asks the engine whether it is rootless.
func (e Engine) Rootless(ctx context.Context) (Report, error) {
	format := "{{.Host.Security.Rootless}}"
	if e.kind == Docker {
		format = "{{.SecurityOptions}}"
	}
	out, code, err := e.info(ctx, format)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Posture:  RootlessVerdict(e.kind, out),
		Command:  string(e.kind) + " info --format '" + format + "'",
		Output:   out,
		ExitCode: code,
	}, nil
}

// SELinuxMode asks getenforce and the engine, and gives the verdict. A getenforce that is not
// installed is a host with no SELinux userland; one that fails says so in text the verdict
// cannot read.
func (e Engine) SELinuxMode(ctx context.Context) (SELinux, error) {
	if err := e.known(); err != nil {
		return SELinuxUnknown, err
	}
	format := "{{.Host.Security.SELinuxEnabled}}"
	if e.kind == Docker {
		format = "{{.SecurityOptions}}"
	}
	enforce, err := getenforce(ctx, e.runner)
	if err != nil {
		return SELinuxUnknown, err
	}
	report, _, err := e.info(ctx, format)
	if err != nil {
		return SELinuxUnknown, err
	}
	if e.kind == Docker {
		report = DockerSELinuxReport(report)
	}
	return SELinuxVerdict(enforce, report), nil
}

// UIDMap is the uid map of podman's user namespace, read from `podman unshare`'s stdout only,
// so a warning podman prints is not read as part of the map.
func (e Engine) UIDMap(ctx context.Context) ([]Range, error) {
	if err := e.known(); err != nil {
		return nil, err
	}
	if e.kind != Podman {
		return nil, fmt.Errorf("%s: %w", e.kind, ErrNoUIDMap)
	}
	res, err := e.run(ctx, "unshare", "cat", "/proc/self/uid_map")
	if err != nil {
		return nil, fmt.Errorf("read the uid map of podman's user namespace: %w", err)
	}
	return ParseUIDMap(string(bytes.TrimSpace(res.Stdout)))
}

// known refuses the zero Engine, the one Engine whose kind New did not check.
func (e Engine) known() error {
	if e.runner == nil {
		return fmt.Errorf("%q: %w", e.kind, ErrUnknownKind)
	}
	return nil
}

// run is one run of the engine's program, with its output captured.
func (e Engine) run(ctx context.Context, args ...string) (execx.Result, error) {
	if err := e.known(); err != nil {
		return execx.Result{}, err
	}
	return e.runner.Run(ctx, e.cmd(args...))
}

// cmd is one run of the engine's program: podman or docker, as New checked.
func (e Engine) cmd(args ...string) execx.Cmd {
	return execx.Cmd{Name: string(e.kind), Args: args}
}

// info is `info --format F`, its output merged: a failed info's error is text the verdicts
// cannot read, which is unknown, which is the safe side. Only an engine that could not be
// started at all is an error.
func (e Engine) info(ctx context.Context, format string) (string, int, error) {
	if err := e.known(); err != nil {
		return "", 0, err
	}
	out, code, err := merged(ctx, e.runner, e.cmd("info", "--format", format))
	if err != nil {
		return "", 0, fmt.Errorf("ask %s for its info: %w", e.kind, err)
	}
	return out, code, nil
}

// getenforce is getenforce's answer, merged with its errors: "" when it is not installed (no
// SELinux userland), its text when it ran and failed, an error only when it could not start.
func getenforce(ctx context.Context, r execx.Runner) (string, error) {
	out, _, err := merged(ctx, r, execx.Cmd{Name: "getenforce"})
	switch {
	case execx.IsNotInstalled(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("ask getenforce: %w", err)
	default:
		return out, nil
	}
}

// merged runs c with its output as one stream and gives that text and the exit status; the
// error is only a command that could not be run at all.
func merged(ctx context.Context, r execx.Runner, c execx.Cmd) (string, int, error) {
	out, err := execx.Combined(ctx, r, c)
	exit, exited := errors.AsType[*execx.ExitError](err)
	switch {
	case exited:
		return out, exit.Code, nil
	case err != nil:
		return "", 0, err
	default:
		return out, 0, nil
	}
}
