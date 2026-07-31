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


def _drm_head_states() -> list[HeadState]:
    heads = []
    for status_path in sorted(glob.glob("/sys/class/drm/card*-DVI-I-*/status")):
        connector_dir = os.path.dirname(status_path)
        name = os.path.basename(connector_dir)
        status = _read(status_path).strip()
        edid_path = os.path.join(connector_dir, "edid")
        edid_bytes = os.path.getsize(edid_path) if os.path.exists(edid_path) else 0
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
    who = subprocess.run(["who"], capture_output=True, text=True, check=False).stdout
    for line in who.splitlines():
        user = line.split()[0] if line.split() else ""
        if not user:
            continue
        uid = subprocess.run(["id", "-u", user], capture_output=True, text=True, check=False)
        if uid.returncode != 0:
            continue
        env = dict(
            os.environ,
            DBUS_SESSION_BUS_ADDRESS=f"unix:path=/run/user/{uid.stdout.strip()}/bus",
        )
        subprocess.run(
            ["sudo", "-u", user, "notify-send", "-u", "critical", "DisplayLink dock", message],
            env=env,
            check=False,
        )


def _observe(attempted_restart: bool, attempted_reauth: bool, attempted_reload: bool) -> SystemState:
    heads = _drm_head_states()
    return SystemState(
        heads=heads,
        service_active=_service_active(),
        mutter_corruption_detected=_mutter_corruption_in_recent_journal(),
        attempted_service_restart=attempted_restart,
        attempted_usb_reauth=attempted_reauth,
        attempted_module_reload=attempted_reload,
        drm_client_active=_drm_client_active(heads),
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
    action = Action.NONE

    try:
        deadline = time.monotonic() + POLL_TIMEOUT_SECONDS
        while True:
            state = _observe(attempted_restart, attempted_reauth, attempted_reload)
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
