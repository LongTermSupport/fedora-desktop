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
BACKGROUND_SCHEMA = "org.gnome.desktop.background"
# Any key in BACKGROUND_SCHEMA emits bg-changed, so the signal is carried by the
# one whose loss is invisible. If this process dies between the nudge and the
# restore, a wrong primary-color sits behind an opaque wallpaper and nobody sees
# it; a lost picture-uri is a desktop with no wallpaper. `finally` does not run
# on SIGTERM, so "we always restore" is not a guarantee that can be made.
SIGNAL_KEY = "primary-color"
RESTORE_ATTEMPTS = 3


def nudged_signal_value(current: str) -> str:
    """A SIGNAL_KEY value guaranteed to differ from `current`.

    dconf suppresses a write that changes nothing, and a suppressed write emits
    no bg-changed — so re-writing the current value would be a silent no-op.
    Both candidates are valid colours, so whichever is chosen is harmless if the
    restore never happens.

    `current` is the GVariant printed form exactly as `gsettings get` returned
    it, quotes included, and is compared and written back unmodified. Stripping
    the quotes makes the value unparseable as a GVariant, at which point
    gsettings falls back to treating it as a literal string — which round-trips
    by luck for plain input and silently corrupts anything containing an escape.
    """
    return "'#ffffff'" if current == "'#000000'" else "'#000000'"


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


def _session_property(session_id: str, prop: str) -> str:
    result = subprocess.run(
        ["loginctl", "show-session", session_id, f"--property={prop}", "--value"],
        capture_output=True,
        text=True,
        check=False,
    )
    return result.stdout.strip() if result.returncode == 0 else ""


def graphical_sessions() -> list[tuple[str, str, str]]:
    """(session_id, username, uid) for every session that can render a desktop.

    Enumerated from loginctl and filtered on session Type, because `who` reports
    neither sessions nor graphical-ness: this host shows a `wayland` session and
    an `unspecified` one for the same user, and only the first has a desktop to
    repaint. Filtering here keeps the lock check and the write operating on the
    same set — checking one session's lock state while writing to every session
    would let a write reach a locked one.
    """
    listed = subprocess.run(
        ["loginctl", "list-sessions", "--no-legend"],
        capture_output=True,
        text=True,
        check=False,
    )
    if listed.returncode != 0:
        return []
    sessions = []
    for line in listed.stdout.splitlines():
        fields = line.split()
        if not fields:
            continue
        session_id = fields[0]
        if _session_property(session_id, "Type") not in ("wayland", "x11"):
            continue
        user = _session_property(session_id, "Name")
        uid = _session_property(session_id, "User")
        if user and uid:
            sessions.append((session_id, user, uid))
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
    sessions = graphical_sessions()
    if not sessions:
        return True
    # Every graphical session, not just the user's "Display" one: the refresh
    # writes to all of them, so a single locked session is enough to skip.
    return any(_session_property(sid, "LockedHint") != "no" for sid, _user, _uid in sessions)


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
    for _sid, user, uid in graphical_sessions():
        read = subprocess.run(
            _as_user(user, uid, "gsettings", "get", BACKGROUND_SCHEMA, SIGNAL_KEY),
            capture_output=True,
            text=True,
            check=False,
        )
        current = read.stdout.strip()
        if read.returncode != 0 or not current:
            print(f"RECOVERY-BACKGROUND-SKIP: no readable {SIGNAL_KEY} for {user}")
            continue

        nudge = subprocess.run(
            _as_user(
                user, uid, "gsettings", "set",
                BACKGROUND_SCHEMA, SIGNAL_KEY, nudged_signal_value(current),
            ),
            check=False,
        )
        if nudge.returncode != 0:
            print(f"RECOVERY-BACKGROUND-SKIP: could not signal {user}, nothing changed")
            continue

        # Restore is check=False and verified by read-back rather than trusted.
        # Raising here would mask whatever sent us into the restore, and an
        # unverified write is the exact defect this whole change exists to fix:
        # gsettings exits zero even when dconf discarded the write.
        restored = False
        for _ in range(RESTORE_ATTEMPTS):
            subprocess.run(
                _as_user(
                    user, uid, "gsettings", "set", BACKGROUND_SCHEMA, SIGNAL_KEY, current
                ),
                check=False,
            )
            back = subprocess.run(
                _as_user(user, uid, "gsettings", "get", BACKGROUND_SCHEMA, SIGNAL_KEY),
                capture_output=True,
                text=True,
                check=False,
            )
            if back.returncode == 0 and back.stdout.strip() == current:
                restored = True
                break
        if restored:
            print(f"RECOVERY-BACKGROUND: refreshed desktop background for {user}")
        else:
            print(
                f"RECOVERY-BACKGROUND-WARN: {SIGNAL_KEY} left as "
                f"{nudged_signal_value(current)} for {user}; it is hidden behind "
                f"the wallpaper — restore with: gsettings set {BACKGROUND_SCHEMA} "
                f"{SIGNAL_KEY} {current}"
            )


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
    for _sid, user, uid in graphical_sessions():
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
