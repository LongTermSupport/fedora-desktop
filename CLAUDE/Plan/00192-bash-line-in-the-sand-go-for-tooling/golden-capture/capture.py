"""Golden capture: run the shipped bash podfreeze and lxcfreeze under fakes and record them.

Plan 00192, freeze port wave 4. Test tooling for a program that has been removed: it runs the
bash tools as they were at a git revision (default: the last commit that still had them), so
the goldens in src-go/internal/freeze/golden/testdata/golden/ can be re-derived and audited.

    python3 -I capture.py [--rev REV] [--out DIR] [scenario ...]

Each scenario in scenarios.json names a world (what the fake podman, sudo, lxc-* and fzf say),
a command line, and optionally a terminal with typed replies. The tool under test is the
shipped file; the only change is that the library copy used by the "bypass" tree ends with
`assert_on_host() { :; }`, so the container guard does not refuse inside the container this
runs in. Scenarios marked "guard": true run the unmodified library.

The same world semantics are implemented again in Go (golden_test.go); the two must agree.
"""

import argparse
import fcntl
import hashlib
import json
import os
import pty
import select
import shutil
import subprocess
import sys
import tempfile
import termios
import time
from pathlib import Path

REPO = Path(__file__).resolve().parents[4]
SCENARIOS = REPO / "src-go/internal/freeze/golden/testdata/scenarios.json"
DEFAULT_REV = "67c20b7f"  # lxcfreeze in Go, the last commit before the bash tools were deleted
TOOLS = {
    "podfreeze": "files/home/.local/bin/podfreeze",
    "lxcfreeze": "files/home/.local/bin/lxcfreeze",
}
LIB = "files/home/.local/lib/freeze/freeze-common.bash"
# Names a fake can stand in for; none of these may be found from the real system.
FAKED = ["podman", "sudo", "fzf", "lxc-ls", "lxc-info", "lxc-freeze", "lxc-unfreeze",
         "lxc-attach", "nmcli", "lxc-top"]
TIMEOUT = 30
# Not "stdout", "status" and the like: qa-changed.bash maps a changed file to the suites that
# mention its name, and those names are mentioned everywhere.
GOLDEN_SUFFIX = ".golden"


def resolve(scenarios, scenario):
    """The scenario's world with its overrides applied (same rule as golden_test.go)."""
    world = json.loads(json.dumps(scenarios["worlds"][scenario["world"]]))
    for key in ("tools", "sudo", "fzf"):
        if key in scenario:
            world[key] = scenario[key]
    commands = world.setdefault("commands", [])
    for extra in scenario.get("commands", []):
        commands[:] = [c for c in commands if c["argv"] != extra["argv"]]
        commands.append(extra)
    world.setdefault("fzf", [])
    return world


def fake_main():
    """Run as a fake program: python3 -I capture.py --fake NAME ARGS..."""
    name, args = sys.argv[2], sys.argv[3:]
    world = json.loads(Path(os.environ["GOLDEN_WORLD"]).read_text())
    state = Path(os.environ["GOLDEN_STATE"])
    code = run_fake(world, state, name, args)
    sys.exit(code)


def log(state, argv):
    with (state / "calls").open("a") as handle:
        handle.write(json.dumps(argv) + "\n")


def bump(state, key):
    path = state / ("count-" + key)
    count = int(path.read_text()) if path.exists() else 0
    path.write_text(str(count + 1))
    return count


def run_fake(world, state, name, args):
    if name == "sudo":
        return run_sudo(world, state, args)
    if name == "fzf":
        return run_fzf(world, state, args)
    return answer(world, state, [name, *args])


def has_terminal():
    """Whether a controlling terminal can be opened, as sudo needs to ask for a password."""
    try:
        os.close(os.open("/dev/tty", os.O_RDWR))
    except OSError:
        return False
    return True


def run_sudo(world, state, args):
    mode = world.get("sudo", "cached")
    noninteractive = bool(args) and args[0] == "-n"
    rest = args[1:] if noninteractive else args
    cached = (state / "sudo-cached").exists() or mode == "cached"
    if not rest or rest[0] in ("-v", "true"):
        if noninteractive:
            if cached:
                return 0
            sys.stderr.write("sudo: a password is required\n")
            return 1
        if not cached and not has_terminal():
            sys.stderr.write("sudo: a terminal is required to read the password\n")
            return 1
        if mode == "refused" and not cached:
            log(state, ["sudo-auth", "refused"])
            sys.stderr.write("sudo: 3 incorrect password attempts\n")
            return 1
        log(state, ["sudo-auth", "ok"])
        (state / "sudo-cached").write_text("1")
        return 0
    if not cached:
        sys.stderr.write("sudo: a password is required\n")
        return 1
    return answer(world, state, rest)


def called(state, argv):
    return (state / ("count-" + hashlib.sha1(json.dumps(argv).encode()).hexdigest())).exists()


def answer(world, state, argv):
    for command in world["commands"]:
        if command["argv"] == argv:
            log(state, argv)
            # The last answer whose "after" command has been run wins; one with no "after" always holds.
            reply = {}
            for candidate in command["answers"]:
                if "after" not in candidate or called(state, candidate["after"]):
                    reply = candidate
            bump(state, hashlib.sha1(json.dumps(argv).encode()).hexdigest())
            sys.stdout.write(reply.get("stdout", ""))
            sys.stderr.write(reply.get("stderr", ""))
            return reply.get("exit", 0)
    log(state, ["!unscripted", *argv])
    sys.stderr.write("fake: unscripted command: " + json.dumps(argv) + "\n")
    return 127


def run_fzf(world, state, args):
    log(state, ["fzf", *args])
    rows = sys.stdin.read().split("\n")
    if rows and rows[-1] == "":
        rows.pop()
    replies = world.get("fzf", [])
    seen = bump(state, "fzf")
    if seen >= len(replies):
        sys.stderr.write("fake: no scripted fzf answer\n")
        return 127
    reply = replies[seen]
    if "pick" in reply:
        for index in reply["pick"]:
            sys.stdout.write(rows[index] + "\n")
        return 0
    return reply.get("exit", 130)


def build_trees(root, rev):
    """Two trees laid out the way the tools find their library: bypass (guard off) and shipped."""
    trees = {}
    for label in ("bypass", "shipped"):
        tree = root / label
        (tree / "bin").mkdir(parents=True)
        (tree / "lib/freeze").mkdir(parents=True)
        for tool, path in TOOLS.items():
            blob = subprocess.run(["git", "-C", str(REPO), "show", f"{rev}:{path}"],
                                  check=True, capture_output=True).stdout
            (tree / "bin" / tool).write_bytes(blob)
            (tree / "bin" / tool).chmod(0o755)
        lib = subprocess.run(["git", "-C", str(REPO), "show", f"{rev}:{LIB}"],
                             check=True, capture_output=True).stdout
        if label == "bypass":
            lib += b"\nassert_on_host() { :; }\n"
        (tree / "lib/freeze/freeze-common.bash").write_bytes(lib)
        trees[label] = tree
    return trees


def real_bin(root):
    """Every program of the system except the ones a fake stands in for."""
    directory = root / "realbin"
    directory.mkdir()
    for source in ("/usr/bin", "/usr/sbin"):
        for entry in sorted(os.listdir(source)):
            if entry not in FAKED and not (directory / entry).exists():
                (directory / entry).symlink_to(Path(source) / entry)
    return directory


def fake_bin(scenario_dir, tools):
    directory = scenario_dir / "fakebin"
    directory.mkdir()
    for name in tools:
        stub = directory / name
        stub.write_text('#!/bin/sh\nexec python3 -I "$GOLDEN_CAPTURE" --fake %s "$@"\n' % name)
        stub.chmod(0o755)
    return directory


def run_scenario(scenario, world, trees, realbin, work):
    scenario_dir = work / scenario["name"]
    state = scenario_dir / "state"
    state.mkdir(parents=True)
    (scenario_dir / "world.json").write_text(json.dumps(world))
    fakes = fake_bin(scenario_dir, world["tools"])
    tree = trees["shipped" if scenario.get("guard") else "bypass"]
    env = {
        "PATH": f"{fakes}:{realbin}",
        "HOME": str(scenario_dir),
        "LC_ALL": "C",
        "TERM": "dumb",
        "GOLDEN_CAPTURE": str(Path(__file__).resolve()),
        "GOLDEN_WORLD": str(scenario_dir / "world.json"),
        "GOLDEN_STATE": str(state),
        **scenario.get("env", {}),
    }
    argv = [str(tree / "bin" / scenario["tool"]), *scenario["args"]]
    stdout_path = scenario_dir / "stdout"
    replies = scenario.get("tty")
    with stdout_path.open("wb") as out:
        if replies is None:
            proc = subprocess.run(argv, env=env, stdin=subprocess.DEVNULL, stdout=out,
                                  stderr=subprocess.PIPE, start_new_session=True,
                                  timeout=TIMEOUT, check=False)
            stderr, status = proc.stderr, proc.returncode
        else:
            stderr, status = run_on_terminal(argv, env, out, replies)
    if status < 0:
        status = 128 - status
    (scenario_dir / "stderr").write_bytes(stderr)
    calls = state / "calls"
    return {
        "stdout": stdout_path.read_bytes(),
        "stderr": stderr,
        "status": f"{status}\n".encode(),
        "calls": calls.read_bytes() if calls.exists() else b"",
    }


def run_on_terminal(argv, env, out, replies):
    """Run with stdin and stderr on a pseudo-terminal that is also the controlling one."""
    master, slave = pty.openpty()
    attrs = termios.tcgetattr(slave)
    attrs[1] &= ~termios.OPOST  # no \n -> \r\n
    attrs[3] &= ~(termios.ECHO | termios.ECHOCTL)
    termios.tcsetattr(slave, termios.TCSANOW, attrs)

    def become_controlling():
        fcntl.ioctl(2, termios.TIOCSCTTY, 0)

    proc = subprocess.Popen(argv, env=env, stdin=slave, stdout=out, stderr=slave,
                            start_new_session=True, preexec_fn=become_controlling)
    os.close(slave)
    typed = "".join(reply + "\n" for reply in replies).encode() + b"\x04"
    os.write(master, typed)
    captured = bytearray()
    deadline = time.monotonic() + TIMEOUT
    while True:
        ready, _, _ = select.select([master], [], [], 0.2)
        if ready:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            captured += chunk
        elif proc.poll() is not None:
            break
        if time.monotonic() > deadline:
            proc.kill()
            raise SystemExit("timeout: " + " ".join(argv))
    status = proc.wait()
    os.close(master)
    return bytes(captured), status


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--fake":
        fake_main()
    parser = argparse.ArgumentParser()
    parser.add_argument("--rev", default=DEFAULT_REV)
    parser.add_argument("--out", default=str(SCENARIOS.parent / "golden"))
    parser.add_argument("names", nargs="*")
    options = parser.parse_args()
    scenarios = json.loads(SCENARIOS.read_text())
    chosen = [s for s in scenarios["scenarios"] if not options.names or s["name"] in options.names]
    out = Path(options.out)
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        trees = build_trees(root, options.rev)
        realbin = real_bin(root)
        work = root / "work"
        for scenario in chosen:
            result = run_scenario(scenario, resolve(scenarios, scenario), trees, realbin, work)
            target = out / scenario["name"]
            if target.exists():
                shutil.rmtree(target)
            target.mkdir(parents=True)
            for stream, data in result.items():
                (target / (stream + GOLDEN_SUFFIX)).write_bytes(data)
            print(scenario["name"], result["status"].decode().strip(), file=sys.stderr)


if __name__ == "__main__":
    main()
