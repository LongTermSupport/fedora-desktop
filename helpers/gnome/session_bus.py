"""Reaching a D-Bus session, for every helper in the GNOME extensions play.

Both the applier and the verifier need a bus, and they resolved one differently
until Plan 00112: the applier tried `DBUS_SESSION_BUS_ADDRESS`, then each runtime
directory tested for access, then `dbus-run-session`; the verifier always derived
`/run/user/<uid>/bus` and never checked it was reachable. Under an Ansible
`become` that leaked a stale `XDG_RUNTIME_DIR`, one of them reached the live
shell and the other did not — and the two then disagreed about what the session
contained. One implementation, used by both.
"""

from __future__ import annotations

import dataclasses
import os
from collections.abc import Mapping, Sequence


@dataclasses.dataclass(frozen=True)
class SessionBus:
    """How to reach a D-Bus session: a command prefix and/or an address."""

    prefix: list[str]
    address: str | None
    source: str


def runtime_dirs(environ: Mapping[str, str], uid: int) -> list[str]:
    """Candidate runtime directories, most specific first.

    `XDG_RUNTIME_DIR` first because it is what a real session sets, then the
    uid-derived path, which is the one derived from who we actually are and so is
    never dropped — `sudo -u` can leave the environment variable pointing at
    another user's `0700` directory.
    """
    candidates = [(environ.get("XDG_RUNTIME_DIR") or "").strip(), f"/run/user/{uid}"]
    seen: set[str] = set()
    ordered: list[str] = []
    for candidate in candidates:
        if candidate and candidate not in seen:
            seen.add(candidate)
            ordered.append(candidate)
    return ordered


def resolve_session_bus(
    environ: Mapping[str, str], candidate_runtime_dirs: Sequence[str]
) -> SessionBus:
    """Pick the bus to use, preferring the user's live one.

    A live session's bus means the running shell sees a change immediately, and
    that a state read is the state a user would see. With no bus at all —
    `run.bash` from a TTY — `dbus-run-session` gives dconf a throwaway one, so a
    write still lands in the user's database and no caller needs to tolerate a
    failure for the no-session case.

    Each socket is tested for ACCESS, not existence: a stale `XDG_RUNTIME_DIR`
    pointing at another user's runtime directory shows a socket that cannot be
    connected to, and falling through to the next candidate succeeds where using
    it would fail.
    """
    address = environ.get("DBUS_SESSION_BUS_ADDRESS", "").strip()
    if address:
        return SessionBus(prefix=[], address=address, source="environment")

    for runtime_dir in candidate_runtime_dirs:
        socket = os.path.join(runtime_dir, "bus")
        if os.access(socket, os.R_OK | os.W_OK):
            return SessionBus(
                prefix=[], address=f"unix:path={socket}", source="runtime-socket"
            )

    return SessionBus(
        prefix=["dbus-run-session", "--"], address=None, source="dbus-run-session"
    )


def env_for(bus: SessionBus, environ: Mapping[str, str]) -> dict[str, str]:
    """A copy of `environ` pointed at this bus.

    Nothing is exported for the `dbus-run-session` route: that command sets the
    variable itself, and pre-setting it would point the child at a bus other than
    the one it just started.
    """
    env = dict(environ)
    if bus.address:
        env["DBUS_SESSION_BUS_ADDRESS"] = bus.address
    return env


def current() -> SessionBus:
    """The bus for this process, resolved from its own environment and uid."""
    return resolve_session_bus(os.environ, runtime_dirs(os.environ, os.getuid()))
