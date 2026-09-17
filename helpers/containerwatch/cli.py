#!/usr/bin/env python3
"""Executor + CLI for the container-process watchdog (Plan 00055).

Thin side-effecting wrapper around the pure logic in core.py. It enumerates the
real `/proc`, samples per-core CPU over a short interval (PID-reuse-guarded),
resolves container ids → friendly names by shelling out to the owning engine
(`podman`/`docker` inspect, lxc by name), writes `report.json` atomically to the
per-user runtime dir, and emits a `gdbus` DBus signal so the GNOME Shell
extension can react. The same `report.json` backs both the panel and the CLI —
one data source, two front-ends.

REPORTING-ONLY: there is no process-termination path in this module or anywhere
in the package. `exec_hint` is a guidance STRING the human may choose to run; the
tool never executes it. The L0 no-kill gate enforces this statically.

Subcommands:

    container-watch scan [--once] [--json] [--inject F] [--interval S]
    container-watch status
    container-watch list
    container-watch explain <host_pid>
    container-watch watch [--interval S]

Invoked as a module from the repo root (playbook/tests) or the deployed wrapper:

    python3 -m helpers.containerwatch.cli scan --once --json
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

from helpers.containerwatch import containment, core, crashloop, restartpolicy

# Production defaults (overridable by config file, then by env — see
# resolve_thresholds). 900 s = 15 min; CPU% is per-single-core (so multi-core
# pinning scores >100% and is instantly legible, e.g. the 1116% incident).
DEFAULT_AGE_S = 900
DEFAULT_CPU_PCT = 50
SAMPLE_INTERVAL_S = 1.0

# DBus signal target — all-lowercase namespace, matching the speech-to-text
# precedent (org.fedoradesktop.SpeechToText).
DBUS_PATH = "/org/fedoradesktop/ContainerWatch"
DBUS_INTERFACE = "org.fedoradesktop.ContainerWatch"

_SUBPROCESS_TIMEOUT_S = 5

# A stop must outlive the grace period it grants the workload, or the timeout
# fires while the container is still shutting down cleanly and the outcome is
# recorded as a failure that did not happen.
_CONTAINMENT_TIMEOUT_S = containment.STOP_TIMEOUT_S + 10


# --------------------------------------------------------------------------- #
# Config + thresholds
# --------------------------------------------------------------------------- #
def config_path() -> str:
    base = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
    return os.path.join(base, "container-watch", "config.json")


def load_config(path: str) -> dict:
    """Load the JSON config (allowlist, optional thresholds); {} if absent."""
    if not os.path.exists(path):
        return {}
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def resolve_thresholds(config: dict) -> tuple[int, float]:
    """Age + CPU thresholds: env (CW_AGE_S/CW_CPU_PCT) > config > default."""
    age_env = os.environ.get("CW_AGE_S")
    age = int(age_env) if age_env else int(config.get("age_s", DEFAULT_AGE_S))
    cpu_env = os.environ.get("CW_CPU_PCT")
    cpu = float(cpu_env) if cpu_env else float(config.get("cpu_pct", DEFAULT_CPU_PCT))
    return age, cpu


# --------------------------------------------------------------------------- #
# /proc helpers (real side-effecting reads)
# --------------------------------------------------------------------------- #
def read_btime(proc_root: str) -> int:
    """Boot time (unix seconds) from /proc/stat `btime` — wall-clock-free age."""
    with open(os.path.join(proc_root, "stat"), encoding="utf-8") as fh:
        for line in fh:
            if line.startswith("btime "):
                return int(line.split()[1])
    raise RuntimeError(f"no btime line in {proc_root}/stat")


def make_cpu_sampler(proc_root: str, interval_s: float, clock_ticks: int):
    """Return a `sampler(pid) -> float | None` that diffs two /proc stat reads.

    Reads stat, sleeps `interval_s`, reads again, and hands both snapshots to
    core.cpu_delta_pct (which guards PID reuse via starttime). Returns None if
    the process vanished between reads — the scan skips it rather than flagging.
    """

    def sampler(pid: int) -> float | None:
        stat = os.path.join(proc_root, str(pid), "stat")
        try:
            with open(stat, encoding="utf-8") as fh:
                before = fh.read()
        except FileNotFoundError:
            return None
        time.sleep(interval_s)
        try:
            with open(stat, encoding="utf-8") as fh:
                after = fh.read()
        except FileNotFoundError:
            return None
        return core.cpu_delta_pct(before, after, interval_s, clock_ticks)

    return sampler


# --------------------------------------------------------------------------- #
# Engine name resolution + presence probe
# --------------------------------------------------------------------------- #
def engine_available(engine: str) -> bool:
    """True if the engine's CLI is on PATH (test harness gates per engine)."""
    binary = {"podman": "podman", "docker": "docker", "lxc": "lxc-info"}.get(engine)
    return bool(binary) and shutil.which(binary) is not None


def resolve_name(attr: core.Attribution) -> str:
    """Resolve a container id → friendly name via the owning engine.

    Rootless Podman is queried natively (the user timer IS the owning uid — no
    uid hop). Resolution failure (daemon down, container already gone, CLI
    missing) falls back to the short id so a scan never crashes on name lookup.
    LXC already carries the plain name in the cgroup path. An "unknown" engine is
    never shelled out to.
    """
    if attr.engine == "lxc":
        return attr.container_token
    if attr.engine == "unknown":
        return f"unknown:{attr.container_token or '?'}"

    if attr.engine == "podman":
        argv = ["podman"] if attr.rootless else ["sudo", "podman"]
        argv += ["inspect", "--format", "{{.Name}}", attr.container_token]
    elif attr.engine == "docker":
        argv = ["docker", "inspect", "--format", "{{.Name}}", attr.container_token]
    else:  # pragma: no cover - defensive
        return attr.container_token[:12]

    try:
        result = subprocess.run(
            argv, check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S
        )
    except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired):
        return attr.container_token[:12]
    return result.stdout.strip().lstrip("/")


def sample_restarts(engine: str) -> tuple[dict, dict, dict] | None:
    """Sample one engine's restart counters, running states and names.

    Returns ``(counts, running, names)`` keyed by container id, or **None** when
    the engine could not be sampled at all.

    None and three empty mappings are deliberately different. Empty means "this
    engine was asked and has no containers"; None means "this engine was not
    asked, or would not answer". Collapsing them would let an engine that failed
    to respond be reported as an engine checked and found clean — the
    partial-result-read-as-complete defect this repo keeps meeting.

    ONE `ps` plus ONE `inspect` for the whole set, not a call per container. The
    scan's cost must not scale with how much the user happens to be running.
    """
    if not engine_available(engine):
        return None

    fmt = "{{.Id}}\t{{.RestartCount}}\t{{.State.Running}}\t{{.Name}}"
    try:
        listed = subprocess.run(
            [engine, "ps", "-aq"],
            check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S,
        )
        ids = [line.strip() for line in listed.stdout.splitlines() if line.strip()]
        if not ids:
            return {}, {}, {}
        result = subprocess.run(
            [engine, "inspect", "--format", fmt, *ids],
            check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S,
        )
    except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired):
        return None

    return crashloop.parse_inspect_lines(result.stdout)


def sample_restart_policy_rows(engine: str) -> list[dict] | None:
    """Sample one engine's configured restart policies, or None if unreachable.

    Same None-vs-empty distinction as `sample_restarts`, for the same reason: an
    engine that would not answer must not read as an engine with nothing to
    report.
    """
    if not engine_available(engine):
        return None

    fmt = (
        "{{.Id}}\t{{.Name}}\t{{.HostConfig.RestartPolicy.Name}}"
        "\t{{.HostConfig.RestartPolicy.MaximumRetryCount}}"
    )
    try:
        listed = subprocess.run(
            [engine, "ps", "-aq"],
            check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S,
        )
        ids = [line.strip() for line in listed.stdout.splitlines() if line.strip()]
        if not ids:
            return []
        result = subprocess.run(
            [engine, "inspect", "--format", fmt, *ids],
            check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S,
        )
    except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired):
        return None

    return restartpolicy.parse_lines(result.stdout)


def audit_all_restart_policies() -> tuple[list[dict], dict]:
    """Restart-policy findings, and the classification of EVERY container.

    Returns ``(findings, classifications)``. The findings are the preventive
    report; the classifications are what containment gates on, so the same
    inspect serves both and the two can never disagree about a container.

    A container missing from `classifications` is one whose policy could not be
    read, and containment treats that as "do not act" — the safe direction.
    """
    findings: list[dict] = []
    classifications: dict[str, str] = {}
    for engine in crashloop.RESTART_CAPABLE_ENGINES:
        rows = sample_restart_policy_rows(engine)
        if rows is None:
            continue
        for row in rows:
            classifications[row["container_id"]] = restartpolicy.classify(
                row["policy"], row["max_retries"]
            )
        findings.extend(restartpolicy.audit(rows, engine=engine))
    return findings, classifications


def containment_enabled(config: dict) -> bool:
    """Whether automatic containment may act. Default ON, deliberately.

    Detection that never acts is the state that let a restart storm take a
    desktop down while a report was being written about it — and on a server
    nobody reads the report at all. Defaulting this off would ship the defence in
    the posture that already failed.

    The measured separation makes on-by-default defensible: the storm ran to
    131,377 restarts while the busiest legitimate container on the same host
    managed 19 in its lifetime. Set `containment: false` to opt out, and the
    allowlist exempts individual containers without disabling the rest.
    """
    value = config.get("containment", True)
    return bool(value) if isinstance(value, bool) else True


def previous_history(report: dict) -> dict:
    """Per-container restart history from the last report, or an empty mapping.

    Anything unreadable degrades to empty rather than raising: a corrupt history
    must cost a window of containment sensitivity, never the whole scan.
    """
    raw = report.get("restart_history")
    if not isinstance(raw, dict):
        return {}
    return {str(k): v for k, v in raw.items() if isinstance(v, list)}


def advance_history(history: dict, *, counts: dict, now: int) -> dict:
    """Append this tick's counts, and drop containers that no longer exist.

    Pruning matters: without it, the history grows for ever on a host that churns
    through short-lived containers, and report.json is read and rewritten every
    tick.
    """
    return {
        cid: containment.record_sample(history.get(cid, []), at=now, count=count)
        for cid, count in counts.items()
    }


def apply_containment(candidates: list[dict], *, now: int, allowlist: list, enabled: bool) -> list[dict]:
    """Stop the containers that have earned it, and record every outcome.

    THE ONLY PLACE IN THIS PACKAGE THAT CHANGES A CONTAINER'S STATE. It is kept
    to a handful of lines, and the decision it acts on is made in the pure
    `containment` module where every branch is tested without a container.

    Outcomes are recorded for refusals too. "Why was this NOT stopped" is the
    question asked after an incident, and a log that only records actions cannot
    answer it.

    Diagnostics go to stderr because a `systemd --user` timer's stderr is the
    journal, which on a SERVER is the only delivery channel there is — no panel,
    no notification, nobody logged in.
    """
    outcomes: list[dict] = []
    for candidate in candidates:
        decision = containment.decide(
            candidate, now=now, allowlist=allowlist, enabled=enabled
        )
        name = candidate.get("container_name", "?")
        if not decision.contain:
            outcomes.append({"container_name": name, "stopped": False, "reason": decision.reason})
            continue

        argv = containment.build_stop_argv(
            engine=candidate["engine"], container_id=candidate["container_id"]
        )
        try:
            subprocess.run(
                argv, check=True, capture_output=True, text=True,
                timeout=_CONTAINMENT_TIMEOUT_S,
            )
        except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired) as exc:
            # A failed stop is louder than a successful one: the host is still in
            # danger and nothing has relieved it.
            print(f"container-watch: CONTAINMENT FAILED for {name}: {exc}", file=sys.stderr)
            outcomes.append({"container_name": name, "stopped": False, "reason": f"stop failed: {exc}"})
            continue

        print(
            f"container-watch: STOPPED {name} — {decision.reason}. "
            "It was restarting fast enough to threaten the whole host.",
            file=sys.stderr,
        )
        outcomes.append({"container_name": name, "stopped": True, "reason": decision.reason})

    return outcomes


def sample_all_engines() -> tuple[dict, dict, dict, dict, dict]:
    """Sample every restart-capable engine, and report which were covered.

    Returns ``(counts, running, names, engines, coverage)``.

    Docker is included because the plan requires it and the watchdog already
    attributes docker containers. An earlier revision defaulted the engine
    argument to podman and never passed one, so a docker container could
    crash-loop and produce a report indistinguishable from a healthy host.
    """
    counts: dict[str, int] = {}
    running: dict[str, bool] = {}
    names: dict[str, str] = {}
    engines: dict[str, str] = {}
    checked: list[str] = []
    available: list[str] = []

    for engine in crashloop.RESTART_CAPABLE_ENGINES:
        if engine_available(engine):
            available.append(engine)
        sampled = sample_restarts(engine)
        if sampled is None:
            continue
        checked.append(engine)
        engine_counts, engine_running, engine_names = sampled
        counts.update(engine_counts)
        running.update(engine_running)
        names.update(engine_names)
        for container_id in engine_counts:
            engines[container_id] = engine

    coverage = crashloop.engine_coverage(checked=checked, available=available)
    return counts, running, names, engines, coverage


# --------------------------------------------------------------------------- #
# Report assembly, atomic write, DBus emission
# --------------------------------------------------------------------------- #
def build_report(
    findings: list,
    host_cores: int,
    age_threshold: int,
    cpu_threshold: float,
    generated_at: int,
    restart_counts: dict | None = None,
    crashloop_coverage: dict | None = None,
    restart_history: dict | None = None,
    containment: list | None = None,
) -> dict:
    """Assemble the report.

    `restart_counts` is this tick's raw per-container restart counter. It is
    persisted so the NEXT tick can difference against it — the report is already
    written every tick and read by the CLI, so it serves as the state store and
    no second one is introduced. Keyword-defaulted so existing callers and their
    tests are unaffected.
    """
    report = {
        "schema": core.SCHEMA_VERSION,
        "generated_at": generated_at,
        "host_cores": host_cores,
        "thresholds": {"age_s": age_threshold, "cpu_pct": cpu_threshold},
        "findings": findings,
    }
    if restart_counts is not None:
        report["restart_counts"] = restart_counts
    if crashloop_coverage is not None:
        report["crashloop_coverage"] = crashloop_coverage
    if restart_history is not None:
        report["restart_history"] = restart_history
    # Recorded even when empty, unlike the fields above: an empty list is the
    # positive statement "containment ran and stopped nothing", which is a
    # different fact from the key being absent because it never ran at all.
    if containment is not None:
        report["containment"] = containment
    return report


def write_report_atomic(path: str, report: dict) -> None:
    """Write report.json atomically (tmp + os.replace) so readers never see a
    half-written file. Creates the parent dir if needed."""
    directory = os.path.dirname(path)
    os.makedirs(directory, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=directory, prefix=".report-", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            json.dump(report, fh, indent=2)
        os.replace(tmp, path)
    except BaseException:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise


def emit_signal(count: int, report_path: str) -> None:
    """Emit the FindingsChanged DBus signal (count + report path).

    Best-effort UI notification: the report.json is already the source of truth,
    so a missing session bus (timer fired headless) must NOT abort the scan. We
    catch the specific subprocess failures and warn on stderr rather than
    swallowing them silently.
    """
    argv = [
        "gdbus", "emit", "--session",
        "--object-path", DBUS_PATH,
        "--signal", f"{DBUS_INTERFACE}.FindingsChanged",
        str(count), report_path,
    ]
    try:
        subprocess.run(argv, check=True, capture_output=True, text=True, timeout=_SUBPROCESS_TIMEOUT_S)
    except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired) as exc:
        print(f"container-watch: DBus emit skipped ({exc.__class__.__name__}: no session bus?)", file=sys.stderr)


# --------------------------------------------------------------------------- #
# Inject seam (deterministic UI/CLI drive without a real runaway)
# --------------------------------------------------------------------------- #
def carry_forward_baseline(previous: dict | None) -> tuple[dict | None, dict | None]:
    """The restart baseline and coverage to re-write when this scan sampled nothing.

    Returns `(None, None)` rather than `({}, {})` when the previous report has no
    baseline to carry. An empty mapping is a positive claim that every container
    stood at zero, which would difference the whole host against zero on the next
    tick and flag all of it; `None` says only that nothing is known, which is the
    truth.
    """
    if not isinstance(previous, dict):
        return None, None
    counts = previous.get("restart_counts")
    coverage = previous.get("crashloop_coverage")
    return (
        counts if isinstance(counts, dict) else None,
        coverage if isinstance(coverage, dict) else None,
    )


def load_injected(spec: str) -> list:
    """Load a synthetic finding (or 'empty') for --inject — the L3 test seam."""
    if spec == "empty":
        return []
    with open(spec, encoding="utf-8") as fh:
        data = json.load(fh)
    return data if isinstance(data, list) else [data]


# --------------------------------------------------------------------------- #
# Rendering
# --------------------------------------------------------------------------- #
def render_status(report: dict) -> str:
    findings = report.get("findings", [])
    n = len(findings)
    if n == 0:
        # "OK" is a claim about the host, and it is only honest for the engines
        # this tick actually reached. An engine that is installed but did not
        # answer leaves a blind spot, and reporting OK over a blind spot is the
        # failure this watchdog exists to avoid.
        unchecked = (report.get("crashloop_coverage") or {}).get("not_checked") or []
        if unchecked:
            return (
                f"container-watch: 0 findings, but NOT CHECKED: {', '.join(unchecked)} "
                "— this is not a clean bill of health"
            )
        return "container-watch: OK — 0 findings"

    # Crash loops are named in the one-line summary rather than folded into a
    # total. This line is what the timer writes to the journal every tick, and
    # "3 findings" reads like the CPU noise the watchdog usually reports —
    # whereas a crash loop is a threat to the whole session and should not have
    # to be discovered by running a second command.
    # An action taken outranks anything merely observed. On a server this line in
    # the journal is the ONLY notice anyone gets that a container was stopped.
    stopped = [o for o in report.get("containment", []) if o.get("stopped")]
    if stopped:
        names = ", ".join(o.get("container_name", "?") for o in stopped)
        return f"container-watch: STOPPED {len(stopped)} crash-looping container(s): {names}"

    loops = sum(1 for f in findings if f.get("kind") == "crashloop")
    if loops:
        return (
            f"container-watch: {loops} CRASH LOOP(S) + {n - loops} other finding(s) "
            "— run `container-watch list`"
        )

    # Policy findings are advisory, and saying "N findings" about them would read
    # like something is wrong now. Named separately so a host whose only issue is
    # configuration is not confused with one that is actually misbehaving.
    policies = sum(1 for f in findings if f.get("kind") == "restart-policy")
    if policies == n:
        return (
            f"container-watch: {policies} container(s) configured to restart without "
            "limit — run `container-watch list`"
        )
    return f"container-watch: {n} finding(s) — run `container-watch list`"


def render_list(report: dict) -> str:
    """Render findings for a human.

    Two SHAPES of finding share this report and they do not share columns: a
    CPU/age finding is about a process (cpu_pct, age_s, cmd), a crash-loop
    finding is about a container and has no process at all. Rendered through the
    process columns a crash-loop finding printed as zeros and a blank command —
    present in the report, invisible to the reader. They are tabulated
    separately, because a detector whose output cannot be read is not a defence.
    """
    findings = report.get("findings", [])
    loops = [f for f in findings if f.get("kind") == "crashloop"]
    policies = [f for f in findings if f.get("kind") == "restart-policy"]
    procs = [f for f in findings if f.get("kind") not in ("crashloop", "restart-policy")]

    lines: list[str] = []

    # Containment first: something was STOPPED on this host, and that outranks
    # every advisory below it. Burying an action the watchdog took under tables of
    # things it merely noticed is how an operator finds out by accident.
    for outcome in report.get("containment", []):
        if outcome.get("stopped"):
            lines.append(f"STOPPED {outcome.get('container_name', '?')} — {outcome.get('reason', '')}")
    if lines:
        lines.append("")

    if loops:
        lines.append("CRASH LOOPS")
        lines.append(
            f"  {'CONTAINER':<24} {'ENGINE':<8} {'RESTARTS':>10} {'PER_MIN':>8}  WHY"
        )
        for f in loops:
            per_min = f.get("restarts_per_min")
            per_min_text = "-" if per_min is None else str(per_min)
            lines.append(
                f"  {f.get('container_name', '?'):<24} {f.get('engine', '?'):<8} "
                f"{f.get('restart_count', 0):>10} {per_min_text:>8}  "
                f"{', '.join(f.get('reasons', []))}"
            )
        lines.append("")
        lines.append(
            "  A container restarting without bound puts every cycle on the session"
        )
        lines.append(
            "  D-Bus. Sustained, that exhausts the per-UID quota and kills the desktop."
        )
        lines.append("  Stop it, or fix why it exits:  podman stop <container>")
        if policies or procs:
            lines.append("")

    if policies:
        lines.append("RESTART POLICIES THAT PERMIT AN UNBOUNDED STORM")
        lines.append(f"  {'CONTAINER':<24} {'ENGINE':<8} {'POLICY':<16} RETRIES")
        for f in policies:
            retries = f.get("max_retries", 0)
            lines.append(
                f"  {f.get('container_name', '?'):<24} {f.get('engine', '?'):<8} "
                f"{f.get('policy', '?'):<16} {retries if retries else '-'}"
            )
        lines.append("")
        lines.append(
            "  `on-failure:N` is the ONLY policy podman caps. `always` and"
        )
        lines.append(
            "  `unless-stopped` retry for ever, and ignore any retry count set"
        )
        lines.append(
            "  alongside them — so a container can look bounded and not be."
        )
        lines.append(
            "  There is no backoff either, so a failing container restarts at"
        )
        lines.append(
            "  engine speed until something gives. Recreate with --restart=on-failure:5"
        )
        if procs:
            lines.append("")

    if procs:
        lines.append(f"{'CONTAINER':<24} {'ENGINE':<8} {'CPU%':>6} {'AGE_S':>7}  CMD")
        for f in procs:
            cmd = f.get("cmd", "")
            if len(cmd) > 50:
                cmd = cmd[:47] + "..."
            lines.append(
                f"{f.get('container_name', '?'):<24} {f.get('engine', '?'):<8} "
                f"{f.get('cpu_pct', 0):>6} {f.get('age_s', 0):>7}  {cmd}"
            )

    if not lines:
        lines.append("No findings.")

    # UNCONDITIONAL, and that is the point. This line was previously appended only
    # when a finding already existed — so the one case it exists for, a clean
    # report a human reads to conclude "the host is fine", was the one case it was
    # suppressed. A scan where every engine failed to answer renders identically
    # to a scan that found nothing wrong unless the report says which engines it
    # actually covered.
    coverage_line = _render_coverage(report.get("crashloop_coverage"))
    if coverage_line:
        lines.append("")
        lines.append(coverage_line)

    return "\n".join(lines)


def _render_coverage(coverage: dict | None) -> str:
    """One line naming what the crash-loop scan did and did not cover."""
    if not coverage:
        return ""
    return (
        "crash-loop coverage: checked "
        f"{', '.join(coverage.get('checked') or ['(none)'])}"
        f" | not checked {', '.join(coverage.get('not_checked') or ['(none)'])}"
        f" | no restart counter {', '.join(coverage.get('unsupported') or ['(none)'])}"
    )


def render_explain(report: dict, host_pid: int) -> str:
    for f in report.get("findings", []):
        if f.get("host_pid") == host_pid:
            return "\n".join(
                [
                    f"host_pid       : {f.get('host_pid')}",
                    f"container_pid  : {f.get('container_pid')}",
                    f"engine         : {f.get('engine')} (rootless={f.get('rootless')})",
                    f"container      : {f.get('container_name')} ({f.get('container_id', '')[:12]})",
                    f"argv0          : {f.get('argv0')}",
                    f"cmd            : {f.get('cmd')}",
                    f"age_s          : {f.get('age_s')}",
                    f"cpu_pct        : {f.get('cpu_pct')}",
                    "",
                    "Inspect inside the container with:",
                    f"  {f.get('exec_hint')}",
                ]
            )
    return f"No finding for host pid {host_pid} in the current report."


# --------------------------------------------------------------------------- #
# State paths
# --------------------------------------------------------------------------- #
def report_path() -> str:
    runtime = os.environ.get("XDG_RUNTIME_DIR") or f"/tmp/container-watch-{os.getuid()}"
    return os.path.join(runtime, "container-watch", "report.json")


def read_report() -> dict:
    path = report_path()
    if not os.path.exists(path):
        return {"findings": []}
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def previous_report_for_rate() -> dict:
    """Last tick's report, for differencing restart counts — never fatal.

    `read_report` is allowed to raise for the `status`/`list`/`explain` commands,
    where an unreadable report IS the answer and a traceback is the honest one.
    The SCAN path is different: the report there is a cache of the previous tick,
    not the thing being asked about, so a truncated or half-written file must not
    stop the scan from finding CPU-pinned processes and writing a fresh one.

    Degrades to an empty report, which `crashloop.previous_sample` reads as "no
    basis to measure a rate" — the absolute gate still applies. The failure is
    announced on stderr rather than swallowed: the timer's stderr lands in the
    journal, so a report that is corrupt every tick is visible rather than silent.
    """
    try:
        return read_report()
    except (OSError, json.JSONDecodeError) as exc:
        print(
            f"container-watch: previous report unreadable ({exc}); "
            "restart-rate gate is inactive this tick",
            file=sys.stderr,
        )
        return {}


# --------------------------------------------------------------------------- #
# Subcommands
# --------------------------------------------------------------------------- #
def _scan_once(interval_s: float, inject: str | None) -> dict:
    config = load_config(config_path())
    age, cpu = resolve_thresholds(config)
    proc_root = os.environ.get("CW_PROC_ROOT", "/proc")

    if inject is not None:
        findings = load_injected(inject)
    else:
        clock = os.sysconf("SC_CLK_TCK")
        findings = core.scan_proc_root(
            proc_root,
            now=int(time.time()),
            btime=read_btime(proc_root),
            clock_ticks=clock,
            age_threshold=age,
            cpu_threshold=cpu,
            cpu_sampler=make_cpu_sampler(proc_root, interval_s, clock),
            name_resolver=resolve_name,
            allowlist=config.get("allowlist", []),
        )

    now = int(time.time())

    # Under --inject nothing is sampled, so this scan has no restart counts of its
    # own — but it still WRITES report.json, atomically replacing the previous
    # tick's file. Omitting the counts therefore erases them exactly as surely as
    # writing {} would, and the rate gate is blind for a full window after anyone
    # exercises the notification path. The previous tick's baseline is carried
    # forward instead: a test seam must not damage production state.
    loop_findings: list = []
    coverage = None
    counts: dict = {}
    if inject is None:
        counts, states, names, engines, coverage = sample_all_engines()

        # The rate gate differences against the PREVIOUS report, so it is silent
        # on the very first tick after install. That is the absolute gate's whole
        # purpose — see helpers/containerwatch/crashloop.py.
        prev_counts, prev_at = crashloop.previous_sample(previous_report_for_rate())
        elapsed = crashloop.elapsed_since(previous_at=prev_at, now=now)
        loop_findings = crashloop.evaluate(
            previous=prev_counts if elapsed is not None else {},
            current=counts,
            running=states,
            elapsed_s=elapsed if elapsed is not None else 0.0,
            names=names,
            engines=engines,
        )
        allowlist = config.get("allowlist", [])
        loop_findings = [
            f for f in loop_findings if not crashloop.matches_allowlist(f, allowlist)
        ]

        # The PREVENTIVE half: which containers are configured so that a storm is
        # possible at all. Independent of whether anything is storming now, and
        # the only part of this defence that helps before something goes wrong.
        policy_findings, policy_class = audit_all_restart_policies()

        # Rolling per-container history, carried in the report that already
        # exists. Containment needs a window, and a single previous sample only
        # yields the interval between two ticks.
        history = advance_history(
            previous_history(previous_report_for_rate()), counts=counts, now=now
        )
        candidates = [
            {
                "container_id": cid,
                "container_name": names.get(cid, cid),
                "engine": engines.get(cid, "podman"),
                "running": states.get(cid, False),
                # Absent means the policy could not be read, which `decide`
                # treats as a refusal — the safe direction.
                "restart_policy": policy_class.get(cid, ""),
                "history": history.get(cid, []),
            }
            for cid in counts
        ]
        outcomes = apply_containment(
            candidates, now=now, allowlist=allowlist, enabled=containment_enabled(config)
        )

    if inject is None:
        write_counts: dict | None = counts
        write_coverage = coverage
        write_history: dict | None = history
    else:
        write_counts, write_coverage = carry_forward_baseline(previous_report_for_rate())
        write_history = previous_history(previous_report_for_rate()) or None
        policy_findings = []
        outcomes = []

    findings = list(findings) + loop_findings + policy_findings
    report = build_report(
        findings,
        os.cpu_count() or 1,
        age,
        cpu,
        now,
        restart_counts=write_counts,
        crashloop_coverage=write_coverage,
        restart_history=write_history,
        containment=outcomes,
    )
    write_report_atomic(report_path(), report)
    emit_signal(len(findings), report_path())
    return report


def cmd_scan(args) -> int:
    report = _scan_once(args.interval, args.inject)
    if args.json:
        print(json.dumps(report))
    else:
        print(render_status(report))
    return 0


def render_policies(rows: list[dict], classifications: dict) -> str:
    """Every container's restart policy, including the ones that are fine.

    A complete list, not just the offenders: this is the command to run to ANSWER
    the question "what is configured where", and a table showing only problems
    cannot distinguish a clean host from an unread one.
    """
    if not rows:
        return "No containers found."

    verdicts = {
        "uncapped": "UNBOUNDED",
        "capped": "ok (capped)",
        "none": "ok (no restart)",
        "unknown": "UNKNOWN",
    }
    lines = [f"{'CONTAINER':<34} {'POLICY':<16} {'RETRIES':>7}  VERDICT"]
    for row in sorted(rows, key=lambda r: (r["policy"], r["container_name"])):
        classification = classifications.get(row["container_id"], "unknown")
        lines.append(
            f"{row['container_name'][:34]:<34} {row['policy']:<16} "
            f"{row['max_retries']:>7}  {verdicts.get(classification, classification)}"
        )

    unbounded = sum(1 for c in classifications.values() if c == "uncapped")
    lines.append("")
    if unbounded:
        lines.append(
            f"{unbounded} container(s) restart without limit. Only these can be"
        )
        lines.append(
            "automatically stopped, and only after a sustained restart storm."
        )
    else:
        lines.append("No container is configured to restart without limit.")
    return "\n".join(lines)


def cmd_policies(args) -> int:
    """Read-only. Runs `inspect` and prints; stops nothing, writes no report."""
    rows: list[dict] = []
    classifications: dict[str, str] = {}
    for engine in crashloop.RESTART_CAPABLE_ENGINES:
        sampled = sample_restart_policy_rows(engine)
        if sampled is None:
            continue
        rows.extend(sampled)
        for row in sampled:
            classifications[row["container_id"]] = restartpolicy.classify(
                row["policy"], row["max_retries"]
            )
    print(render_policies(rows, classifications))
    return 0


def cmd_status(args) -> int:
    print(render_status(read_report()))
    return 0


def cmd_list(args) -> int:
    print(render_list(read_report()))
    return 0


def cmd_explain(args) -> int:
    print(render_explain(read_report(), args.host_pid))
    return 0


def cmd_watch(args) -> int:
    try:
        while True:
            report = _scan_once(args.interval, None)
            print("\x1b[2J\x1b[H" if sys.stderr.isatty() else "")
            print(render_list(report))
            time.sleep(args.refresh)
    except KeyboardInterrupt:
        return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="container-watch", description="Reporting-only container runaway watchdog")
    sub = parser.add_subparsers(dest="command", required=True)

    p_scan = sub.add_parser("scan", help="run a detection pass, write report, emit signal")
    p_scan.add_argument("--once", action="store_true", help="single pass (default; kept for clarity)")
    p_scan.add_argument("--json", action="store_true", help="print the full report JSON to stdout")
    p_scan.add_argument("--inject", metavar="FILE|empty", help="write a synthetic finding (test seam)")
    p_scan.add_argument("--interval", type=float, default=SAMPLE_INTERVAL_S, help="CPU sample interval seconds")
    p_scan.set_defaults(func=cmd_scan)

    sub.add_parser("status", help="one-line summary").set_defaults(func=cmd_status)
    sub.add_parser(
        "policies", help="restart policy of every container (read-only, acts on nothing)"
    ).set_defaults(func=cmd_policies)
    sub.add_parser("list", help="table of current findings").set_defaults(func=cmd_list)

    p_explain = sub.add_parser("explain", help="full detail + exec hint for a host pid")
    p_explain.add_argument("host_pid", type=int)
    p_explain.set_defaults(func=cmd_explain)

    p_watch = sub.add_parser("watch", help="live-refresh loop for terminal use")
    p_watch.add_argument("--interval", type=float, default=SAMPLE_INTERVAL_S, help="CPU sample interval seconds")
    p_watch.add_argument("--refresh", type=float, default=5.0, help="seconds between passes")
    p_watch.set_defaults(func=cmd_watch)

    return parser


def main(argv=None) -> int:
    args = build_parser().parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
