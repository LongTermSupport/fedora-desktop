"""Pure logic for the container-process watchdog — stdlib-only, fully unit-tested.

This module is the heart of Plan 00055: it attributes a process to its container
from the world-readable cgroup-v2 path, computes age + per-core CPU%, translates
the host PID to the in-container PID (NSpid), assembles a schema-v1 finding, and
builds the engine-correct `exec_hint` that guides a human to inspect the offender
*inside* its container. It is **reporting-only** — there is deliberately NO
process-termination path anywhere in this package (enforced by the L0 no-kill
gate). The thin side-effecting executor (real `/proc`, `podman/docker/lxc`
name resolution, atomic `report.json` write, `gdbus` signal emission, the CLI)
lives in cli.py.

`scan_proc_root` reads from an injectable `proc_root` (default `/proc` in cli.py)
and takes the CPU sampler and name resolver as callables, so the entire
attribution + detection pipeline is exercised offline from fixture trees with no
containers and no privileges (see tests/helpers/containerwatch/test_core.py).

cgroup attribution markers (cgroup v2, Fedora default) per context.md §6:

    podman rootless  …/user-<uid>.slice/…/libpod-<id>.scope/…
    podman rootful   /machine.slice/libpod-<id>.scope/…
    docker (systemd) /system.slice/docker-<id>.scope
    docker (cgroupfs)/docker/<id>
    lxc / lxd        …/lxc.payload.<name>/…  or  /lxc/<name>

cgroup v1 is unsupported and fails fast (CgroupV1Error) rather than risk a
silent mis-attribution.
"""

from __future__ import annotations

import fnmatch
import os
import re
from dataclasses import dataclass

SCHEMA_VERSION = 1

# A process is attributed to a container engine purely from its cgroup path; we
# never trust /proc/<pid>/comm (the native Claude binary spawned `ugrep` without
# resetting comm to "claude.exe" — context.md §2d gotcha 1). Matching/display use
# /proc/<pid>/cmdline argv[0] instead.

_LIBPOD_RE = re.compile(r"libpod-(?P<id>[0-9a-fA-F]+)\.scope")
_DOCKER_SCOPE_RE = re.compile(r"docker-(?P<id>[0-9a-fA-F]+)\.scope")
_DOCKER_CGROUPFS_RE = re.compile(r"/docker/(?P<id>[0-9a-fA-F]+)")
_LXC_PAYLOAD_RE = re.compile(r"lxc\.payload\.(?P<name>[^/]+)")
_LXC_PLAIN_RE = re.compile(r"/lxc/(?P<name>[^/]+)")
_USER_SLICE_RE = re.compile(r"user-(?P<uid>\d+)\.slice")
# Container runtimes we can see but do not engine-resolve → "unknown" bucket
# (logged, never mistaken for a host process).
_UNKNOWN_MARKERS = ("kubepods", "crio-", "cri-containerd", "containerd-")


class CgroupV1Error(RuntimeError):
    """Raised when a /proc/<pid>/cgroup is cgroup-v1 (unsupported — fail fast)."""


@dataclass
class Attribution:
    """The container a process belongs to, derived from its cgroup path."""

    engine: str  # "podman" | "docker" | "lxc" | "unknown"
    rootless: bool
    owner_uid: int
    container_token: str  # 64-hex id (podman/docker) or plain name (lxc)


def parse_cgroup(text: str) -> Attribution | None:
    """Attribute a /proc/<pid>/cgroup body to a container, or None if host.

    Returns an Attribution for a recognised (or "unknown") container, None for a
    plain host process, and raises CgroupV1Error for a cgroup-v1 hierarchy.
    """
    lines = [ln for ln in text.strip().splitlines() if ln]
    v2 = [ln for ln in lines if ln.startswith("0::")]
    if not v2:
        # No unified (0::) line. If we see controller-hierarchy lines, this is a
        # cgroup-v1 host — unsupported, fail fast rather than mis-attribute.
        v1 = [ln for ln in lines if re.match(r"^\d+:[^:]+:", ln)]
        if v1:
            raise CgroupV1Error("cgroup v1 hierarchy is unsupported")
        return None

    path = v2[0][len("0::"):]

    m = _LIBPOD_RE.search(path)
    if m:
        uid_m = _USER_SLICE_RE.search(path)
        rootless = uid_m is not None
        owner_uid = int(uid_m.group("uid")) if uid_m else 0
        return Attribution("podman", rootless, owner_uid, m.group("id"))

    m = _DOCKER_SCOPE_RE.search(path) or _DOCKER_CGROUPFS_RE.search(path)
    if m:
        return Attribution("docker", False, 0, m.group("id"))

    m = _LXC_PAYLOAD_RE.search(path) or _LXC_PLAIN_RE.search(path)
    if m:
        return Attribution("lxc", False, 0, m.group("name"))

    if any(marker in path for marker in _UNKNOWN_MARKERS):
        return Attribution("unknown", False, 0, "")

    return None


def argv0_basename(cmdline: bytes) -> str:
    """basename of argv[0] from a NUL-separated /proc/<pid>/cmdline (or "")."""
    if not cmdline:
        return ""
    argv0 = cmdline.split(b"\x00", 1)[0].decode("utf-8", "replace")
    return argv0.rsplit("/", 1)[-1]


def cmdline_to_cmd(cmdline: bytes) -> str:
    """Human-readable command string from a NUL-separated cmdline."""
    if not cmdline:
        return ""
    parts = [p.decode("utf-8", "replace") for p in cmdline.split(b"\x00") if p]
    return " ".join(parts)


def _fields_after_comm(stat_text: str) -> list[str]:
    """Return /proc/<pid>/stat fields 3..N, splitting on the LAST ')'.

    comm (field 2) is parenthesised and may itself contain spaces/parens, so we
    locate the final ')' and split the remainder. Field 3 (state) is index 0.
    """
    close = stat_text.rfind(")")
    return stat_text[close + 1:].split()


def parse_starttime(stat_text: str) -> int:
    """Field 22 (starttime, in clock ticks since boot)."""
    return int(_fields_after_comm(stat_text)[19])


def parse_utime_stime(stat_text: str) -> tuple[int, int]:
    """Fields 14 (utime) and 15 (stime), in clock ticks."""
    fields = _fields_after_comm(stat_text)
    return int(fields[11]), int(fields[12])


def parse_nspid(status_text: str) -> int | None:
    """In-container PID from /proc/<pid>/status NSpid (context.md §2c).

    NSpid lists the PID at each namespace level: host first, then nested. We
    return the SECOND field (first nested level) and None when there is only one
    field (process is not in a child PID namespace) or NSpid is absent. Never 0.
    """
    for line in status_text.splitlines():
        if line.startswith("NSpid:"):
            fields = line.split()[1:]
            if len(fields) >= 2:
                return int(fields[1])
            return None
    return None


def parse_vmrss_kb(status_text: str) -> int | None:
    """VmRSS in kB from /proc/<pid>/status, or None if absent."""
    for line in status_text.splitlines():
        if line.startswith("VmRSS:"):
            return int(line.split()[1])
    return None


def compute_age_s(starttime_ticks: int, btime: int, now_unix: int, clock_ticks: int) -> int:
    """Process age in seconds: now - (btime + starttime/HZ). Wall-clock-free."""
    start_unix = btime + (starttime_ticks / clock_ticks)
    return int(now_unix - start_unix)


def compute_cpu_pct(ticks_delta: int, interval_s: float, clock_ticks: int) -> float:
    """Per-core CPU%: core-seconds consumed / wall interval * 100.

    Expressed as a fraction of a SINGLE core, so an N-core-pinned process scores
    ~N*100% (the incident was 1116% ≈ 11 cores) — instantly legible.
    """
    core_seconds = ticks_delta / clock_ticks
    return (core_seconds / interval_s) * 100.0


def cpu_delta_pct(
    before_stat: str, after_stat: str, interval_s: float, clock_ticks: int
) -> float | None:
    """Per-core CPU% from two /proc/<pid>/stat snapshots taken `interval_s` apart.

    Guards PID reuse (context.md §2d / Task 1.2): if `starttime` (field 22)
    differs between the two reads the PID was recycled onto a different process
    mid-sample, so the delta is meaningless → return None (skip, don't flag). A
    negative delta (counter went backwards, e.g. the process exited and the read
    raced) is likewise treated as None.
    """
    if parse_starttime(before_stat) != parse_starttime(after_stat):
        return None
    u0, s0 = parse_utime_stime(before_stat)
    u1, s1 = parse_utime_stime(after_stat)
    delta = (u1 + s1) - (u0 + s0)
    if delta < 0:
        return None
    return compute_cpu_pct(delta, interval_s, clock_ticks)


def is_flagged(age_s: int, cpu_pct: float, age_threshold: int, cpu_threshold: float) -> bool:
    """A process is flagged only when it is BOTH old enough AND hot enough."""
    return age_s >= age_threshold and cpu_pct >= cpu_threshold


def build_exec_hint(attr: Attribution, container_name: str, container_pid: int | None, host_pid: int) -> str:
    """The engine-correct command to inspect the offender inside its container.

    This is guidance text only — a STRING the human may run; the tool never
    executes it (reporting-only). When the in-container PID is unknown the `-p`
    filter is dropped so the hint still lists the container's processes.
    """
    ps = "ps -o pid,%cpu,args"
    if container_pid is not None:
        ps += f" -p {container_pid}"

    if attr.engine == "podman":
        prefix = "podman exec -it" if attr.rootless else "sudo podman exec -it"
        return f"{prefix} {container_name} {ps}"
    if attr.engine == "docker":
        return f"docker exec -it {container_name} {ps}"
    if attr.engine == "lxc":
        return f"sudo lxc-attach -n {container_name} -- {ps}"
    # unknown engine: cannot exec; point the human at the host-side cgroup.
    return f"# engine 'unknown' — inspect host pid {host_pid}: cat /proc/{host_pid}/cgroup"


def make_finding(
    *,
    host_pid: int,
    container_pid: int | None,
    attr: Attribution,
    argv0: str,
    cmd: str,
    age_s: int,
    cpu_pct: float,
    rss_kb: int | None,
    container_name: str,
    exec_hint: str,
) -> dict:
    """Assemble one schema-v1 finding dict (the shared UI/CLI data shape)."""
    return {
        "host_pid": host_pid,
        "container_pid": container_pid,  # null (never 0) when untranslated
        "engine": attr.engine,
        "rootless": attr.rootless,
        "owner_uid": attr.owner_uid,
        "container_id": attr.container_token,
        "container_name": container_name,
        "argv0": argv0,
        "cmd": cmd,
        "age_s": age_s,
        "cpu_pct": int(round(cpu_pct)),
        "rss_kb": rss_kb,
        "exec_hint": exec_hint,
    }


def matches_allowlist(finding: dict, allowlist: list[dict]) -> bool:
    """True if `finding` matches any allowlist entry (so it is suppressed).

    An entry may carry `container_name` (exact match) and/or `cmd_pattern` (an
    fnmatch glob against the full cmd). When both are present BOTH must match;
    an empty entry matches nothing (avoids an accidental allow-all).
    """
    name = finding.get("container_name", "")
    cmd = finding.get("cmd", "")
    for entry in allowlist:
        want_name = entry.get("container_name")
        want_cmd = entry.get("cmd_pattern")
        if want_name is None and want_cmd is None:
            continue
        if want_name is not None and want_name != name:
            continue
        if want_cmd is not None and not fnmatch.fnmatch(cmd, want_cmd):
            continue
        return True
    return False


def _read_text(path: str) -> str:
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read()


def _read_bytes(path: str) -> bytes:
    with open(path, "rb") as fh:
        return fh.read()


def scan_proc_root(
    proc_root: str,
    *,
    now: int,
    btime: int,
    clock_ticks: int,
    age_threshold: int,
    cpu_threshold: float,
    cpu_sampler,
    name_resolver,
    allowlist: list[dict],
) -> list[dict]:
    """Enumerate `proc_root`, flag old+hot container processes, return findings.

    `cpu_sampler(pid) -> float | None` returns the per-core CPU% for a pid (the
    real one in cli.py takes two /proc samples; tests inject a dict). A None
    result means the process vanished mid-scan → skip it (no crash, no false
    flag). `name_resolver(attr) -> str` resolves the friendly container name
    (real one shells out to the engine; tests stub it).

    Order of cheap gates first: attribute → age gate → CPU gate → enrich. A
    process that is not in a container, or younger than the age threshold, never
    reaches the CPU sampler.
    """
    findings: list[dict] = []
    for entry in sorted(os.listdir(proc_root)):
        if not entry.isdigit():
            continue
        pid = int(entry)
        base = os.path.join(proc_root, entry)
        try:
            attr = parse_cgroup(_read_text(os.path.join(base, "cgroup")))
        except CgroupV1Error:
            raise
        except FileNotFoundError:
            continue  # process exited between listdir and read
        if attr is None:
            continue

        try:
            starttime = parse_starttime(_read_text(os.path.join(base, "stat")))
        except (FileNotFoundError, IndexError, ValueError):
            continue
        age_s = compute_age_s(starttime, btime, now, clock_ticks)
        if age_s < age_threshold:
            continue

        cpu_pct = cpu_sampler(pid)
        if cpu_pct is None or cpu_pct < cpu_threshold:
            continue

        try:
            cmdline = _read_bytes(os.path.join(base, "cmdline"))
            status = _read_text(os.path.join(base, "status"))
        except FileNotFoundError:
            continue

        container_pid = parse_nspid(status)
        container_name = name_resolver(attr)
        exec_hint = build_exec_hint(attr, container_name, container_pid, pid)
        finding = make_finding(
            host_pid=pid,
            container_pid=container_pid,
            attr=attr,
            argv0=argv0_basename(cmdline),
            cmd=cmdline_to_cmd(cmdline),
            age_s=age_s,
            cpu_pct=cpu_pct,
            rss_kb=parse_vmrss_kb(status),
            container_name=container_name,
            exec_hint=exec_hint,
        )
        if matches_allowlist(finding, allowlist):
            continue
        findings.append(finding)

    return findings
