#!/usr/bin/env python3
"""Thin executor for DisplayLink dock hotplug recovery.

Gathers real system state (sysfs, journalctl, /proc), asks recovery.decide()
what to do, executes it, and loops (bounded) until the head recovers or every
safe option is exhausted. All decision logic lives in recovery.py and is unit
tested; this module only shells out / touches the filesystem.

Invoked either by udev (on dock USB add) or by the suspend/resume watchdog, as
a module from the deployed helpers tree:

    python3 -m helpers.displaylink_recovery.run_recovery

Runs as root (system udev/systemd context). No dock-identifying info is passed
in via udev environment variables — the dock is rediscovered by USB
vendor/product ID each run, so this also works as a manual diagnostic
(`sudo python3 -m helpers.displaylink_recovery.run_recovery --dry-run`).
"""

from __future__ import annotations

import argparse
import fcntl
import glob
import os
import subprocess
import sys
import time

from helpers.displaylink_recovery.recovery import Action, HeadState, SystemState, decide

DISPLAYLINK_VENDOR_ID = "17e9"
DISPLAYLINK_PRODUCT_ID = "602b"
LOCK_PATH = "/run/displaylink-dock-recovery.lock"
MUTTER_ASSERTIONS = (
    "meta_monitor_manager_get_logical_monitor_from_number",
    "meta_workspace_get_work_area_for_monitor",
)
POLL_INTERVAL_SECONDS = 3
POLL_TIMEOUT_SECONDS = 15


def _read(path: str) -> str:
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def edid_byte_count(edid_path: str) -> int:
    """Bytes of EDID actually present at a sysfs connector path.

    The file MUST be read. `os.path.getsize()` (and `stat`) report **0 for every
    sysfs binary attribute** regardless of content — the inode carries no size —
    so sizing this by stat marks every connected head as having no EDID, which
    is the wedge signature. Every head then looks wedged on every system,
    including a perfectly healthy one, and the recovery ladder runs
    service-restart into USB-reauth into module-reload against working monitors.

    A real connector here reads 256-384 bytes while reporting st_size 0.
    """
    try:
        with open(edid_path, "rb") as f:
            return len(f.read())
    except FileNotFoundError:
        return 0
    except OSError:
        # An unreadable connector is not evidence of a missing EDID, and
        # claiming 0 here would assert the wedge signature on no evidence.
        return -1


def _drm_head_states() -> list[HeadState]:
    heads = []
    for status_path in sorted(glob.glob("/sys/class/drm/card*-DVI-I-*/status")):
        connector_dir = os.path.dirname(status_path)
        name = os.path.basename(connector_dir)
        status = _read(status_path).strip()
        edid_bytes = edid_byte_count(os.path.join(connector_dir, "edid"))
        heads.append(HeadState(name=name, status=status, edid_bytes=edid_bytes))
    return heads


def _service_active() -> bool:
    result = subprocess.run(
        ["systemctl", "is-active", "displaylink-driver.service"],
        capture_output=True,
        text=True,
        check=False,
    )
    return result.stdout.strip() == "active"


def _mutter_corruption_in_recent_journal() -> bool:
    result = subprocess.run(
        ["journalctl", "--since", "2 minutes ago", "--no-pager", "-q"],
        capture_output=True,
        text=True,
        check=True,
    )
    return any(assertion in result.stdout for assertion in MUTTER_ASSERTIONS)


def _drm_client_active(heads: list[HeadState]) -> bool:
    """True if any process holds a /dev/dri/cardN fd for one of the given heads."""
    card_numbers = {h.name.split("-")[0].removeprefix("card") for h in heads}
    if not card_numbers:
        return False
    device_paths = {f"/dev/dri/card{n}" for n in card_numbers}
    for fd_dir in glob.glob("/proc/[0-9]*/fd"):
        try:
            entries = os.listdir(fd_dir)
        except OSError:
            continue
        for entry in entries:
            try:
                target = os.readlink(os.path.join(fd_dir, entry))
            except OSError:
                continue
            if target in device_paths:
                return True
    return False


def _dock_usb_sysfs_dir() -> str | None:
    for vendor_path in glob.glob("/sys/bus/usb/devices/*/idVendor"):
        device_dir = os.path.dirname(vendor_path)
        if _read(vendor_path).strip() != DISPLAYLINK_VENDOR_ID:
            continue
        product_path = os.path.join(device_dir, "idProduct")
        if os.path.exists(product_path) and _read(product_path).strip() == DISPLAYLINK_PRODUCT_ID:
            return device_dir
    return None


def _restart_service() -> None:
    subprocess.run(["systemctl", "restart", "displaylink-driver.service"], check=True)


def _usb_reauth() -> None:
    device_dir = _dock_usb_sysfs_dir()
    if device_dir is None:
        print("RECOVERY-WARN: dock USB device not found for reauth, skipping")
        return
    authorized_path = os.path.join(device_dir, "authorized")
    with open(authorized_path, "w", encoding="utf-8") as f:
        f.write("0")
    time.sleep(1)
    with open(authorized_path, "w", encoding="utf-8") as f:
        f.write("1")


def _module_reload() -> None:
    subprocess.run(["modprobe", "-r", "evdi"], check=True)
    subprocess.run(["modprobe", "evdi"], check=True)


def _graphical_sessions() -> list[tuple[str, str]]:
    """(username, uid) for every logged-in user, for reaching a session bus."""
    sessions = []
    who = subprocess.run(["who"], capture_output=True, text=True, check=False).stdout
    for line in who.splitlines():
        fields = line.split()
        if not fields:
            continue
        user = fields[0]
        uid = subprocess.run(["id", "-u", user], capture_output=True, text=True, check=False)
        if uid.returncode != 0:
            continue
        entry = (user, uid.stdout.strip())
        if entry not in sessions:
            sessions.append(entry)
    return sessions


def _as_user(user: str, uid: str, *command: str) -> list[str]:
    """argv running `command` as `user` with their session bus reachable.

    The bus address must travel THROUGH sudo via `env`, not via subprocess's
    own `env=`: sudo sanitises the environment it hands to the child, so a
    DBUS_SESSION_BUS_ADDRESS set on this process is dropped. dconf then tries to
    autolaunch a private bus, fails with "Failed to execute child process
    dbus-launch", and every write is silently lost while the command still exits
    zero — a change that reports success and does nothing.
    """
    return [
        "sudo",
        "-u",
        user,
        "env",
        f"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/{uid}/bus",
        *command,
    ]


def _session_locked() -> bool:
    """True if any graphical session is locked.

    Conservative: an unreadable lock state counts as locked, because the cost of
    refreshing when we should not (a ~57 MB per-monitor leak, gnome-shell#9188)
    is much worse than the cost of skipping a cosmetic repaint.
    """
    sessions = _graphical_sessions()
    if not sessions:
        return True
    for _user, uid in sessions:
        result = subprocess.run(
            ["loginctl", "show-user", uid, "--property=Display", "--value"],
            capture_output=True,
            text=True,
            check=False,
        )
        session_id = result.stdout.strip()
        if result.returncode != 0 or not session_id:
            return True
        locked = subprocess.run(
            ["loginctl", "show-session", session_id, "--property=LockedHint", "--value"],
            capture_output=True,
            text=True,
            check=False,
        )
        if locked.returncode != 0 or locked.stdout.strip() != "no":
            return True
    return False


def _refresh_background() -> None:
    """Force a `bg-changed` so mutter repaints the desktop background.

    Works around mutter#4767: after a monitor change the background paint is
    skipped on some heads and they show black, while the Overview — drawing the
    same texture — renders correctly. Only a genuine `bg-changed` re-sets
    CHANGED_BACKGROUND; another monitors-changed does not.

    The key is blanked and restored rather than rewritten to its current value,
    because dconf suppresses a write that does not change anything and no signal
    would be emitted. The restore is in a `finally` so an exception mid-toggle
    cannot leave the user with no wallpaper.
    """
    schema = "org.gnome.desktop.background"
    for user, uid in _graphical_sessions():
        for key in ("picture-uri", "picture-uri-dark"):
            read = subprocess.run(
                _as_user(user, uid, "gsettings", "get", schema, key),
                capture_output=True,
                text=True,
                check=False,
            )
            current = read.stdout.strip()
            if read.returncode != 0 or not current or current == "''":
                continue
            try:
                subprocess.run(
                    _as_user(user, uid, "gsettings", "set", schema, key, ""),
                    check=True,
                )
            finally:
                subprocess.run(
                    _as_user(user, uid, "gsettings", "set", schema, key, current.strip("'")),
                    check=True,
                )
        print(f"RECOVERY-BACKGROUND: refreshed desktop background for {user}")


def _notify(message: str) -> None:
    print(f"RECOVERY-NOTIFY: {message}")
    subprocess.run(
        ["systemd-cat", "-t", "displaylink-dock-recovery", "-p", "warning"],
        input=message,
        text=True,
        check=False,
    )
    # Best-effort desktop notification to every graphical user session; a
    # missing/unreachable session bus must not fail the recovery run.
    for user, uid in _graphical_sessions():
        subprocess.run(
            _as_user(
                user, uid, "notify-send", "-u", "critical", "DisplayLink dock", message
            ),
            check=False,
        )


def _observe(
    attempted_restart: bool,
    attempted_reauth: bool,
    attempted_reload: bool,
    attempted_background: bool,
) -> SystemState:
    heads = _drm_head_states()
    return SystemState(
        heads=heads,
        service_active=_service_active(),
        mutter_corruption_detected=_mutter_corruption_in_recent_journal(),
        attempted_service_restart=attempted_restart,
        attempted_usb_reauth=attempted_reauth,
        attempted_module_reload=attempted_reload,
        drm_client_active=_drm_client_active(heads),
        attempted_background_refresh=attempted_background,
        session_locked=_session_locked(),
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Observe and print the decided action without executing it.",
    )
    args = parser.parse_args(argv)

    lock_fd = os.open(LOCK_PATH, os.O_CREAT | os.O_RDWR, 0o644)
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        print("RECOVERY-SKIP: another recovery run is already in progress")
        return 0

    attempted_restart = False
    attempted_reauth = False
    attempted_reload = False
    attempted_background = False
    action = Action.NONE

    try:
        deadline = time.monotonic() + POLL_TIMEOUT_SECONDS
        while True:
            state = _observe(
                attempted_restart, attempted_reauth, attempted_reload, attempted_background
            )
            action = decide(state)
            print(f"RECOVERY-STATE: action={action.value}")

            if action == Action.NONE:
                break
            if args.dry_run:
                print(f"RECOVERY-DRY-RUN: would execute {action.value}")
                break
            if action == Action.RESTART_SERVICE:
                _restart_service()
                attempted_restart = True
            elif action == Action.USB_REAUTH:
                _usb_reauth()
                attempted_reauth = True
            elif action == Action.MODULE_RELOAD:
                _module_reload()
                attempted_reload = True
            elif action == Action.REFRESH_BACKGROUND:
                _refresh_background()
                attempted_background = True
            elif action == Action.NOTIFY_MUTTER_CORRUPTION:
                _notify(
                    "Display state may be corrupted after a dock hotplug — "
                    "log out and back in to fully recover."
                )
                break

            if time.monotonic() > deadline:
                print("RECOVERY-TIMEOUT: giving up after polling window")
                break
            time.sleep(POLL_INTERVAL_SECONDS)
    finally:
        fcntl.flock(lock_fd, fcntl.LOCK_UN)
        os.close(lock_fd)

    print(f"RECOVERY-DONE: final_action={action.value}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
