#!/usr/bin/env python3
"""Run SPEC-reclaim.md section 6's scenario table against a reclaim implementation.

  parity.py bash [REV]   the bash reclaim at REV (default 842d88ddd), from git
  parity.py go           the Go reclaim, built from this checkout's src-go

Each scenario runs in a scratch home with fake programs on PATH that record their argv (sudo
taken off and noted as root), under a pseudo-terminal for /dev/tty. The verdict compares only
the exit status, the commands that change the machine and the files left in the home, never
wording. A scenario with bashDiffers is expected to differ under the bash: that is reported,
and a match there is reported as stale. Any other difference exits 1.
"""

import json
import os
import pty
import select
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", "..", ".."))
TABLE = os.path.join(ROOT, "src-go", "internal", "reclaim", "testdata", "scenarios.json")
REAL_TOOLS = ["bash", "env", "mktemp", "rm", "uname", "awk", "sort", "head", "numfmt", "find", "grep", "cat", "tr"]

STUB = """#!%s -I
import json, os, sys
name, args = os.path.basename(sys.argv[0]), sys.argv[1:]
if name == "id":
    print(1000)
    sys.exit(0)
root = False
if name == "sudo":
    if args in (["-n", "-v"], ["-n", "true"], ["-v"], ["-k"]):
        sys.exit(0)
    if args and args[0] == "-n":
        args = args[1:]
    name, args, root = args[0], args[1:], True
argv = [name] + args
with open(os.environ["RP_LOG"], "a") as log:
    log.write(json.dumps({"argv": argv, "root": root}) + "\\n")
for rule in reversed(json.loads(os.environ["RP_RULES"])):
    if argv[:len(rule["argv"])] == rule["argv"]:
        sys.stdout.write(rule.get("stdout", ""))
        sys.stderr.write(rule.get("stderr", ""))
        sys.exit(rule.get("exit", 0))
sys.exit(0)
"""


def build(impl, rev, work):
    target = os.path.join(work, "reclaim")
    if impl == "bash":
        src = subprocess.run(["git", "-C", ROOT, "show", rev + ":files/home/.local/bin/reclaim"],
                             check=True, capture_output=True).stdout
        with open(target, "wb") as f:
            f.write(src)
        os.chmod(target, 0o755)
    else:
        subprocess.run(["go", "build", "-o", target, "./cmd/reclaim"], cwd=os.path.join(ROOT, "src-go"), check=True)
    return target


def world(sc, work):
    bindir, realdir, home = (os.path.join(work, d) for d in ("bin", "real", "home"))
    for d in (bindir, realdir, home):
        os.makedirs(d)
    stub = os.path.join(work, "stub")
    with open(stub, "w") as f:
        f.write(STUB % sys.executable)
    os.chmod(stub, 0o755)
    for prog in sc.get("installed", []) + ["id", "setsid"]:
        os.symlink(stub, os.path.join(bindir, prog))
    for tool in REAL_TOOLS:
        os.symlink(shutil.which(tool), os.path.join(realdir, tool))
    for rel in sc.get("home", []):
        p = os.path.join(home, rel)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w") as f:
            f.write("x")
    log = os.path.join(work, "log")
    env = {"HOME": home, "PATH": bindir + ":" + realdir, "NO_COLOR": "1", "TERM": "dumb",
           "RP_LOG": log, "RP_RULES": json.dumps(sc.get("programs", []))}
    return home, log, env


def run_pty(argv, env, typed):
    pid, fd = pty.fork()
    if pid == 0:
        os.execve(argv[0], argv, env)
    os.write(fd, ("".join(line + "\n" for line in typed)).encode() + b"\x04" * 6)
    deadline = time.time() + 30
    while time.time() < deadline:
        ready, _, _ = select.select([fd], [], [], 0.5)
        if ready:
            try:
                if not os.read(fd, 65536):
                    break
            except OSError:
                break
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            return os.waitstatus_to_exitcode(status)
    while time.time() < deadline:
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            return os.waitstatus_to_exitcode(status)
        time.sleep(0.05)
    os.kill(pid, 9)
    os.waitpid(pid, 0)
    return "timeout"


def run_scenario(target, sc, changing):
    work = tempfile.mkdtemp(prefix="reclaim-parity-")
    try:
        home, log, env = world(sc, work)
        argv = [target] + sc.get("args", [])
        if sc.get("noTty"):
            code = subprocess.run(argv, env=env, stdin=subprocess.DEVNULL, capture_output=True,
                                  start_new_session=True, timeout=30).returncode
        else:
            code = run_pty(argv, env, sc.get("typed", []))
        changes = []
        if os.path.exists(log):
            with open(log) as f:
                for line in f:
                    entry = json.loads(line)
                    text = " ".join(entry["argv"])
                    if any(text.startswith(p) for p in changing):
                        changes.append(("root " if entry["root"] else "") + text)
        left = sorted(os.path.relpath(os.path.join(d, n), home)
                      for d, _, names in os.walk(home) for n in names)
        return {"exit": code, "changes": changes, "homeAfter": left}
    finally:
        shutil.rmtree(work)


def main():
    impl = sys.argv[1] if len(sys.argv) > 1 else ""
    if impl not in ("bash", "go"):
        sys.exit("usage: parity.py bash [REV] | go")
    rev = sys.argv[2] if len(sys.argv) > 2 else "842d88ddd"
    with open(TABLE) as f:
        table = json.load(f)
    build_dir = tempfile.mkdtemp(prefix="reclaim-impl-")
    unexplained = 0
    try:
        target = build(impl, rev, build_dir)
        for sc in table["scenarios"]:
            want = {"exit": sc.get("exit", 0), "changes": sc.get("changes", []), "homeAfter": sc.get("homeAfter", [])}
            got = run_scenario(target, sc, table["changing"])
            differs = sc.get("bashDiffers") if impl == "bash" else None
            if got == want:
                verdict = "STALE: matches although bashDiffers says it differs" if differs else "same"
                unexplained += bool(differs)
            elif differs:
                verdict = "differs as expected (" + differs + "): " + json.dumps(got)
            else:
                verdict = "DIFFERS: got " + json.dumps(got) + " want " + json.dumps(want)
                unexplained += 1
            print(f"{sc['name']}: {verdict}")
    finally:
        shutil.rmtree(build_dir)
    print(f"{impl}: {len(table['scenarios'])} scenarios, {unexplained} unexplained")
    sys.exit(1 if unexplained else 0)


if __name__ == "__main__":
    main()
