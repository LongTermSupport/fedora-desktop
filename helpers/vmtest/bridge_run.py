"""The body of a dispatched bridge run scope (Plan 00110, DESIGN.md §6.4 step 14, §6.5, §6.6).

The watcher wrote the signed `accepted` response and the in-flight lock, then
handed this argv to `systemd-run --user --scope`. This process owns the
request from here to its terminal response:

    python3 -m helpers.vmtest.bridge_run --checkout DIR --slug SLUG \\
        --config-dir DIR --state-dir DIR --request NAME --verb VERB [--argument ARG]

1. pin the spool (responses/, processing/, archive/) — refused ⇒ audit, exit 2
2. read the accepted stub back through the pinned fd and VERIFY it: signed by
   this key, state accepted, and naming this request/verb/argument; anything
   else is a refusal (the argv is the watcher's, the stub must agree with it)
3. write `running`; refresh its heartbeat every --heartbeat-seconds while the
   verb executes, so a reader can tell "busy" from "dead" (§6.5)
4. run the verb — `run-scenario` is the deployed `vmtest run <scenario>`, a
   hardcoded argv, never a shell
5. archive the run's transcript and judged response into the spool through a
   fresh pinned archive directory; a refused archive is a finished `error` at
   stage collect, never a silent omission
6. write the signed `finished` response, remove the processing entry, clear the
   in-flight lock — the lock is cleared on EVERY exit path, so a crash here
   never wedges the bridge

Silence is never an outcome: a verb this revision does not implement finishes
as `error` saying so. Exit 0 on a pass, 1 on fail/error, 2 on a refusal.
"""

from __future__ import annotations

import argparse
import errno
import json
import os
import pathlib
import re
import subprocess
import sys
import time

from helpers.vmtest import spool, verdict

DEFAULT_VMTEST = pathlib.Path.home() / ".local" / "bin" / "vmtest"
DEFAULT_HEARTBEAT_SECONDS = 60.0
RUN_LINE_RE = re.compile(r"^VMTEST-RUN (\S+) verdict=(pass|fail|error) response=(\S+)$", re.MULTILINE)
ARCHIVED_FILES = ("transcript.log", "response.json", "console.log")

# The last `==> …` progress line the CLI printed before it died names the stage
# it died in; a `die` before any progress line is the CLI refusing to start.
STAGE_MARKERS = (
    (re.compile(r"^==> freshness:"), "freshness"),
    (re.compile(r"^==> run \S+: booting"), "boot"),
    (re.compile(r"^==> (waiting for SSH|guest answered SSH)"), "ssh"),
    (re.compile(r"^==> (fetching run\.bash|provisioning)"), "provision"),
    (re.compile(r"^RUN-BASH-EXIT "), "assert"),
    (re.compile(r"^==> verdict:"), "collect"),
)
DIE_STAGES = (
    (re.compile(r"not in the deployed allowlist|is not runnable"), "allowlist"),
    (re.compile(r"\(stage: base\)|freshness:|unknown provenance"), "base"),
)


class Refusal(RuntimeError):
    """The stub, the key or the spool cannot be trusted; nothing more is written into the spool."""


def vmtest_home() -> pathlib.Path:
    return pathlib.Path(
        os.environ.get("VMTEST_HOME", str(pathlib.Path.home() / ".local" / "share" / "vmtest"))
    )


def host_only_refusal(home: pathlib.Path, scenario_id: str) -> str | None:
    """Why `scenario_id` must not run from the bridge, or None if it may (Plan 00121).

    Read from the deployed MANIFEST, deliberately not from `scenarios.allowlist`.
    The allowlist is one generated file, and a control whose only input is a file
    something else writes fails open the moment that file is wrong — which is the
    whole defect class this check exists to close. A host-only scenario handles a
    real credential, and `run_scenario` copies the transcript and console log onto
    the shared mount, so a wrong answer here builds the credential channel the
    bridge exists to prevent.

    Fails closed on every uncertainty: an unreadable manifest, an absent entry,
    and anything in `host_only` that is not literally `false` all refuse. "We
    could not tell" is not "it is fine".
    """
    path = home / "scenarios.json"
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        return (
            f"cannot read the deployed manifest {path} to establish whether {scenario_id!r} "
            f"is host_only ({exc}); refusing rather than assuming it is not"
        )
    entry = (document.get("vm_test_scenarios") or {}).get(scenario_id)
    if not isinstance(entry, dict):
        return (
            f"the deployed manifest does not describe {scenario_id!r}, so it cannot be shown "
            "to be safe to run from the bridge; re-run play-vm-test-lab.yml on the host"
        )
    if entry.get("host_only", False) is not False:
        return (
            f"{scenario_id!r} is host_only: it handles a real credential, so it is run by a "
            "human at the host CLI and its artefacts stay off the shared mount. A sandboxed "
            "agent asking the host to put a credential into a VM is the shape the bridge "
            "exists to prevent (DESIGN.md:2084)."
        )
    return None


class Clock:
    """Real time, or a fixed `--now` advanced by elapsed monotonic time so tests are deterministic."""

    def __init__(self, fixed: int | None) -> None:
        self.fixed = fixed
        self.start = time.monotonic()

    def now(self) -> int:
        if self.fixed is None:
            return int(time.time())
        return self.fixed + int(time.monotonic() - self.start)


class Audit:
    def __init__(self, path: pathlib.Path) -> None:
        self.path = path

    def record(self, now: int, event: str, request: str, detail: str = "") -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(f"{now} {event} {request} {detail}".rstrip() + "\n")


def read_key(config_dir: pathlib.Path) -> bytes:
    path = config_dir / "response.key"
    try:
        key = path.read_bytes()
    except OSError as exc:
        raise Refusal(f"no signing key at {path}: {exc}") from exc
    if len(key) < verdict.MIN_KEY_BYTES:
        raise Refusal(f"signing key at {path} is shorter than {verdict.MIN_KEY_BYTES} bytes")
    return key


def read_pinned(dir_fd: int, name: str) -> bytes:
    """Read a regular file through a pinned directory fd, never following a symlink."""
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=dir_fd)
    except OSError as exc:
        raise Refusal(f"{name}: {os.strerror(exc.errno)}") from exc
    try:
        chunks = []
        while True:
            chunk = os.read(fd, 65536)
            if not chunk:
                return b"".join(chunks)
            chunks.append(chunk)
    finally:
        os.close(fd)


def load_stub(responses_fd: int, key: bytes, args: argparse.Namespace) -> dict:
    text = read_pinned(responses_fd, f"{args.request}.response.json").decode("utf-8")
    try:
        stub = verdict.verify(text, key)
    except verdict.SignatureError as exc:
        raise Refusal(f"accepted stub for {args.request} does not verify: {exc}") from exc
    expected = {"state": verdict.STATE_ACCEPTED, "request": args.request, "verb": args.verb, "argument": args.argument}
    for field, value in expected.items():
        if stub.get(field) != value:
            raise Refusal(f"accepted stub {field}={stub.get(field)!r} does not match this dispatch ({value!r})")
    if not isinstance(stub.get("run_id"), str) or not spool.RUN_ID_RE.match(stub["run_id"]):
        raise Refusal("accepted stub has no usable run_id")
    return stub


def infer_failure(log_text: str) -> tuple[str, str]:
    """Where and why `vmtest run` died without printing its VMTEST-RUN line."""
    lines = [line for line in log_text.splitlines() if line.strip()]
    stage = "boot"
    for line in lines:
        for pattern, name in STAGE_MARKERS:
            if pattern.search(line):
                stage = name
    errors = [line for line in lines if line.startswith("ERROR:")]
    reason = errors[-1] if errors else (lines[-1] if lines else "vmtest printed nothing")
    if not any(pattern.search(line) for line in lines for pattern, _ in STAGE_MARKERS):
        for pattern, name in DIE_STAGES:
            if pattern.search(reason):
                stage = name
    return stage, reason


class Scope:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.clock = Clock(args.now)
        self.audit = Audit(pathlib.Path(args.state_dir) / "service.log")
        self.in_flight = pathlib.Path(args.state_dir) / "in-flight"
        self.key = read_key(pathlib.Path(args.config_dir))
        self.fds: dict[str, int] = {}
        self.document: dict = {}

    # ── spool plumbing ────────────────────────────────────────────────────────────────

    def pin(self) -> None:
        root_fd = spool.open_root(self.args.checkout)
        self.fds["root"] = root_fd
        for name in ("responses", "processing", "archive"):
            self.fds[name] = spool.open_subdir(root_fd, name)

    def close(self) -> None:
        for fd in self.fds.values():
            os.close(fd)
        self.fds.clear()

    def write_response(self, document: dict) -> None:
        self.document = document
        nonce = spool.REQUEST_NAME_RE.match(self.args.request).group(3)
        spool.write_file(self.fds["responses"], f"{self.args.request}.response.json", verdict.sign(document, self.key, nonce=nonce).encode())

    def clear_lock(self) -> None:
        try:
            self.in_flight.unlink()
        except FileNotFoundError:
            self.audit.record(self.clock.now(), "lock-missing", self.args.request, "in-flight lock was already gone")

    # ── the verbs ─────────────────────────────────────────────────────────────────────

    def run_scenario(self, stub: dict) -> dict:
        run_id = stub["run_id"]
        # Before anything boots. Refusing after the run would already have put a
        # real credential in a guest and a transcript on disk.
        refusal = host_only_refusal(vmtest_home(), self.args.argument)
        if refusal is not None:
            self.audit.record(self.clock.now(), "host-only-refused", self.args.request, refusal)
            return verdict.errored(self.document, now=self.clock.now(), stage="allowlist", reason=refusal)
        log_dir = pathlib.Path(self.args.state_dir) / "runs"
        log_dir.mkdir(parents=True, exist_ok=True)
        log_path = log_dir / f"{run_id}.log"
        argv = [str(self.args.vmtest), "run", self.args.argument]
        with log_path.open("wb") as log:
            proc = subprocess.Popen(argv, stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)
            while True:
                try:
                    proc.wait(timeout=self.args.heartbeat_seconds)
                    break
                except subprocess.TimeoutExpired:
                    self.write_response(verdict.running(self.document, now=self.clock.now()))
                    self.audit.record(self.clock.now(), "heartbeat", self.args.request, run_id)
        log_text = log_path.read_text(encoding="utf-8", errors="replace")
        match = RUN_LINE_RE.search(log_text)
        if match is None:
            stage, reason = infer_failure(log_text)
            self.audit.record(self.clock.now(), "vmtest-died", self.args.request, f"exit={proc.returncode} stage={stage}")
            return verdict.errored(self.document, now=self.clock.now(), stage=stage, reason=f"vmtest exited {proc.returncode} during {stage}: {reason}")
        response_path = pathlib.Path(match.group(3))
        judged = json.loads(response_path.read_text(encoding="utf-8"))
        # The CLI names the run; the bridge's run_id must be the same name or the
        # archive and the response would disagree. judge_run copied --run-id in.
        judged["run_id"] = run_id
        final = verdict.finished(self.document, judged)
        try:
            archive_fd = spool.create_archive_dir(self.fds["archive"], run_id)
        except spool.SpoolRefusal as exc:
            self.audit.record(self.clock.now(), "archive-refused", self.args.request, str(exc))
            return verdict.errored(self.document, now=self.clock.now(), stage="collect", reason=f"archive refused: {exc}")
        try:
            for name in ARCHIVED_FILES:
                source = response_path.parent / name
                if source.exists():
                    spool.write_file(archive_fd, name, source.read_bytes())
        finally:
            os.close(archive_fd)
        evidence = dict(final.get("evidence") or {})
        evidence["transcript"] = f"untracked/vmtest-bridge/archive/{run_id}/transcript.log"
        evidence["archive"] = f"untracked/vmtest-bridge/archive/{run_id}"
        final["evidence"] = evidence
        return final

    def refresh_base(self, stub: dict) -> dict:
        """`vmtest refresh-base <server|desktop|all>`: a rebuild, so the same heartbeat loop; no judge."""
        run_id = stub["run_id"]
        log_dir = pathlib.Path(self.args.state_dir) / "runs"
        log_dir.mkdir(parents=True, exist_ok=True)
        log_path = log_dir / f"{run_id}.log"
        with log_path.open("wb") as log:
            proc = subprocess.Popen([str(self.args.vmtest), "refresh-base", self.args.argument], stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)
            while True:
                try:
                    proc.wait(timeout=self.args.heartbeat_seconds)
                    break
                except subprocess.TimeoutExpired:
                    self.write_response(verdict.running(self.document, now=self.clock.now()))
                    self.audit.record(self.clock.now(), "heartbeat", self.args.request, run_id)
        log_text = log_path.read_text(encoding="utf-8", errors="replace")
        built = [line for line in log_text.splitlines() if line.startswith("VMTEST-BASE-BUILT ")]
        if proc.returncode != 0 or not any(line.startswith("VMTEST-REFRESH-DONE ") for line in log_text.splitlines()):
            stage, reason = infer_failure(log_text)
            return verdict.errored(self.document, now=self.clock.now(), stage="base", reason=f"vmtest refresh-base exited {proc.returncode} during {stage}: {reason}")
        now = self.clock.now()
        judged = {
            "state": verdict.STATE_FINISHED,
            "verdict": "pass",
            "verb": stub["verb"],
            "argument": stub["argument"],
            "run_id": run_id,
            "finished_at": verdict._iso(now),
            "checks": {"planned": None, "total": None, "passed": None, "failed": None, "skipped": None},
            "failure": None,
            "evidence": {"bases_built": built},
        }
        return verdict.finished(self.document, judged)

    def list_scenarios(self, stub: dict) -> dict:
        home = vmtest_home()
        manifest = json.loads((home / "scenarios.json").read_text(encoding="utf-8"))
        # Host-only scenarios are omitted: this is the menu the bridge offers the
        # sandbox, and listing an item it may not order would only hand it the
        # exact name to aim a request at.
        declared = manifest.get("vm_test_scenarios", {}) or {}
        offered = {
            scenario_id: entry
            for scenario_id, entry in declared.items()
            if not (isinstance(entry, dict) and entry.get("host_only", False) is not False)
        }
        allowlist_path = home / "scenarios.allowlist"
        allowlist = [line.strip() for line in allowlist_path.read_text(encoding="utf-8").splitlines() if line.strip()] if allowlist_path.exists() else []
        now = self.clock.now()
        judged = {
            "state": verdict.STATE_FINISHED,
            "verdict": "pass",
            "verb": stub["verb"],
            "argument": stub["argument"],
            "run_id": stub["run_id"],
            "finished_at": verdict._iso(now),
            "checks": {"planned": None, "total": None, "passed": None, "failed": None, "skipped": None},
            "failure": None,
            "evidence": {"scenarios": offered, "allowlist": allowlist},
        }
        return verdict.finished(self.document, judged)

    def not_implemented(self, stub: dict) -> dict:
        return verdict.errored(
            self.document,
            now=self.clock.now(),
            stage="allowlist",
            reason=f"verb {stub['verb']!r} is not implemented by this revision of the bridge",
        )

    # ── the lifecycle ─────────────────────────────────────────────────────────────────

    def execute(self) -> int:
        stub = load_stub(self.fds["responses"], self.key, self.args)
        self.write_response(verdict.running(stub, now=self.clock.now()))
        self.audit.record(self.clock.now(), "running", self.args.request, stub["run_id"])
        handlers = {"run-scenario": self.run_scenario, "list-scenarios": self.list_scenarios, "refresh-base": self.refresh_base}
        try:
            final = handlers.get(self.args.verb, self.not_implemented)(stub)
        except Exception as exc:
            # The scope is the last line of defence: a crash here still answers.
            final = verdict.errored(self.document, now=self.clock.now(), stage="collect", reason=f"bridge run crashed: {type(exc).__name__}: {exc}")
            self.write_response(final)
            self.audit.record(self.clock.now(), "finished", self.args.request, f"error crashed {type(exc).__name__}")
            raise
        self.write_response(final)
        self.audit.record(self.clock.now(), "finished", self.args.request, f"{final['verdict']} {stub['run_id']}")
        try:
            os.unlink(self.args.request, dir_fd=self.fds["processing"])
        except OSError as exc:
            if exc.errno != errno.ENOENT:
                raise
            self.audit.record(self.clock.now(), "processing-missing", self.args.request, "the processing entry was already gone")
        print(f"VMTEST-BRIDGE-FINISHED {self.args.request} verdict={final['verdict']}")
        return 0 if final["verdict"] == "pass" else 1

    def run(self) -> int:
        try:
            self.pin()
            return self.execute()
        finally:
            self.close()
            self.clear_lock()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--checkout", required=True)
    parser.add_argument("--slug", required=True)
    parser.add_argument("--config-dir", required=True)
    parser.add_argument("--state-dir", required=True)
    parser.add_argument("--request", required=True)
    parser.add_argument("--verb", required=True)
    parser.add_argument("--argument", default=None)
    parser.add_argument("--vmtest", default=str(DEFAULT_VMTEST), help="the deployed vmtest CLI")
    parser.add_argument("--heartbeat-seconds", type=float, default=DEFAULT_HEARTBEAT_SECONDS)
    parser.add_argument("--now", type=int, default=None)
    args = parser.parse_args(argv)
    if not spool.REQUEST_NAME_RE.match(args.request):
        print(f"REFUSED: {args.request!r} is not a request name", file=sys.stderr)
        return 2
    if args.verb not in spool.VERBS:
        print(f"REFUSED: {args.verb!r} is not a bridge verb", file=sys.stderr)
        return 2

    audit = Audit(pathlib.Path(args.state_dir) / "service.log")
    try:
        return Scope(args).run()
    except (Refusal, spool.SpoolRefusal) as exc:
        audit.record(int(time.time()), "refused", args.request, str(exc))
        print(f"REFUSED: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
