# Fixture for .semgrep/ready-wait.yml (Python). `ruleid:` marks a line that must fire,
# `ok:` a line that must stay silent.
import asyncio
import subprocess
import sys
import time


def start_server_unkept(cmd, is_server_running):
    # The originating instance in wsi-stream: the handle is dropped, so the loop cannot
    # see a server that died loading its model.
    subprocess.Popen(cmd, stdout=subprocess.DEVNULL, start_new_session=True)
    # ruleid: ready-wait-ignores-child-exit
    for _ in range(90):
        time.sleep(0.5)
        if is_server_running():
            return True
    return False


def start_server_kept_unasked(cmd, is_server_running):
    server = subprocess.Popen(cmd)
    # ruleid: ready-wait-ignores-child-exit
    while not is_server_running():
        time.sleep(0.5)
    return server


def start_server_checked(cmd, is_server_running):
    server = subprocess.Popen(cmd)
    # ok: ready-wait-ignores-child-exit
    for _ in range(90):
        time.sleep(0.5)
        if is_server_running():
            return True
        if server.poll() is not None:
            raise RuntimeError(f"the server exited with status {server.returncode}")
    return False


def copy_to_clipboard(text):
    # Cleared in wsi-stream: one short pause, then the exit status is read. No loop.
    proc = subprocess.Popen(["wl-copy"], stdin=subprocess.PIPE)
    proc.stdin.write(text.encode())
    proc.stdin.close()
    time.sleep(0.1)
    return proc.poll() in (None, 0)


def poll_a_service_nobody_here_started(ping):
    # ok: ready-wait-ignores-child-exit
    for _ in range(10):
        if ping():
            return True
        time.sleep(1)
    return False


async def start_async(cmd, ready):
    proc = await asyncio.create_subprocess_exec(*cmd)
    # ruleid: ready-wait-ignores-child-exit
    while not ready():
        await asyncio.sleep(0.1)
    return proc


def supervise_logged_only(rt, argv, environ, err, backoff, final):
    # The originating instance in pingbus's `_supervise` (issue #116): the exit reaches
    # watch.log only, so a failure no restart can fix loops every 60 s for good.
    # ruleid: restart-loop-hides-its-exits
    while True:
        code = rt.run_child(argv, environ)
        if code in final:
            return code
        err.write(f"the watcher exited (exit {code}): starting it again\n")
        rt.sleep(backoff.next())


def restart_for_ever(cmd):
    # ruleid: restart-loop-hides-its-exits
    while True:
        code = subprocess.run(cmd, check=False).returncode
        print(f"exited {code}", file=sys.stderr)
        time.sleep(5)


def supervise_recorded(rt, argv, environ, err, backoff, _record_child_exit):
    # ok: restart-loop-hides-its-exits
    while True:
        code = rt.run_child(argv, environ)
        if _record_child_exit(code):
            err.write("a restart cannot change it: the supervisor exits\n")
            return code
        rt.sleep(backoff.next())


def supervise_recorded_without_a_way_out(rt, argv, environ, backoff, record_exit):
    # ruleid: restart-loop-hides-its-exits
    while True:
        code = rt.run_child(argv, environ)
        record_exit(code)
        rt.sleep(backoff.next())


def run_once(cmd):
    # ok: restart-loop-hides-its-exits
    code = subprocess.run(cmd, check=False).returncode
    time.sleep(1)
    return code


async def start_async_checked(cmd, ready):
    proc = await asyncio.create_subprocess_exec(*cmd)
    # ok: ready-wait-ignores-child-exit
    while not ready():
        if proc.returncode is not None:
            raise RuntimeError(f"exited with status {proc.returncode}")
        await asyncio.sleep(0.1)
    return proc
