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

from helpers.containerwatch import core

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


# --------------------------------------------------------------------------- #
# Report assembly, atomic write, DBus emission
# --------------------------------------------------------------------------- #
def build_report(
    findings: list, host_cores: int, age_threshold: int, cpu_threshold: float, generated_at: int
) -> dict:
    return {
        "schema": core.SCHEMA_VERSION,
        "generated_at": generated_at,
        "host_cores": host_cores,
        "thresholds": {"age_s": age_threshold, "cpu_pct": cpu_threshold},
        "findings": findings,
    }


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
    n = len(report.get("findings", []))
    if n == 0:
        return "container-watch: OK — 0 findings"
    return f"container-watch: {n} finding(s) — run `container-watch list`"


def render_list(report: dict) -> str:
    findings = report.get("findings", [])
    if not findings:
        return "No findings."
    lines = [f"{'CONTAINER':<24} {'ENGINE':<8} {'CPU%':>6} {'AGE_S':>7}  CMD"]
    for f in findings:
        cmd = f.get("cmd", "")
        if len(cmd) > 50:
            cmd = cmd[:47] + "..."
        lines.append(
            f"{f.get('container_name', '?'):<24} {f.get('engine', '?'):<8} "
            f"{f.get('cpu_pct', 0):>6} {f.get('age_s', 0):>7}  {cmd}"
        )
    return "\n".join(lines)


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

    report = build_report(findings, os.cpu_count() or 1, age, cpu, int(time.time()))
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
