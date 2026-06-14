"""Pure verdict logic for a GNOME Shell extension's runtime state.

No I/O — given the live `gnome-extensions info` State, the running GNOME major
version, and the on-disk metadata `shell-version` list, decide whether the play
should pass, pass-with-notice, or fail.

The subtlety this encodes: on Wayland, GNOME Shell cannot reload without ending
the session, so `gnome-extensions info` reports the *running* session's cached
verdict. After a metadata fix is deployed, the live State can still read
"OUT OF DATE" until the user logs out and back in — even though the on-disk
metadata is already correct. That case must NOT fail the play; only a genuine
version mismatch (metadata really does not cover the running shell) or a load
ERROR should. install_pyenv_versions-style: side effects live in
verify_extension.py; the decision lives here and is unit-tested.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass

# gnome-extensions prints the human form "OUT OF DATE"; the D-Bus enum name is
# OUT_OF_DATE. Accept both so the caller need not normalise.
_OUT_OF_DATE = {"OUT OF DATE", "OUT_OF_DATE"}


class Verdict(enum.Enum):
    OK = "ok"
    PENDING_RELOAD = "pending_reload"
    SKIP_NO_SESSION = "skip_no_session"
    FAIL_VERSION = "fail_version"
    FAIL_ERROR = "fail_error"


@dataclass(frozen=True)
class Classification:
    verdict: Verdict
    message: str

    @property
    def is_failure(self) -> bool:
        return self.verdict in (Verdict.FAIL_VERSION, Verdict.FAIL_ERROR)

    @property
    def exit_code(self) -> int:
        return 1 if self.is_failure else 0


def classify(
    *,
    uuid: str,
    session_available: bool,
    live_state: str | None,
    shell_major: str | None,
    metadata_shell_versions: list[str],
) -> Classification:
    """Decide the verdict for one extension. See module docstring for the why."""
    if not session_available:
        return Classification(
            Verdict.SKIP_NO_SESSION,
            f"{uuid}: no GNOME session / D-Bus available — skipping live state check.",
        )

    state = (live_state or "").strip().upper()

    if state == "ERROR":
        return Classification(
            Verdict.FAIL_ERROR,
            f"{uuid}: State ERROR — the extension failed to load. Check "
            "`journalctl --user -b /usr/bin/gnome-shell` for the stack trace.",
        )

    if state in _OUT_OF_DATE:
        if shell_major and shell_major in metadata_shell_versions:
            return Classification(
                Verdict.PENDING_RELOAD,
                f"{uuid}: metadata.json on disk already supports GNOME {shell_major}, "
                "but the running session still reports OUT OF DATE. This is the Wayland "
                "reload lag — log out and log back in to apply. Not failing the play.",
            )
        return Classification(
            Verdict.FAIL_VERSION,
            f"{uuid}: State OUT OF DATE and metadata shell-version "
            f"{metadata_shell_versions} does not cover the running GNOME "
            f"{shell_major or 'unknown'} — add the running major to metadata.json.",
        )

    return Classification(Verdict.OK, f"{uuid}: State {state or 'UNKNOWN'} — OK.")
