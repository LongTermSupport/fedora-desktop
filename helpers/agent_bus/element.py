"""Element Desktop team profiles for a human's desktop user (Plan 00161, DESIGN.md section 8).

Per team, the Flatpak `im.riot.Riot` reads `--profile <team>`'s config from
`~/.var/app/im.riot.Riot/config/Element-<team>/config.json`. This module writes that file
with the locked-down keys of the clients research (subagent-reports/
261006-research-clients-opus-5-5.md §1.4): Element merges a profile's top-level keys over
the bundled element.io config shallowly, so every outbound feature is overridden
explicitly rather than omitted. It also seeds `electron-config.json` with the spell checker
off when that store is absent (Chromium otherwise fetches dictionaries from Google), and
writes a launcher per team.

A human's Element session token lives in that profile, and any process of the same user
can read it. So a user who holds pingbus bundles (`~/.config/pingbus/`, the default
`PINGBUS_HOME`) is refused here, just as `pingbus config check` and `agent-bus-claude`
refuse a `host` member beside an Element profile (`config.ELEMENT_FLATPAK_DIR`).

Usage, as the human's desktop user, the profiles as JSON on stdin
(`[{"team": ..., "base_url": ..., "server_name": ...}]`, `server_name` optional):

    python3 -m helpers.agent_bus.element check   # validate and refuse; writes nothing
    python3 -m helpers.agent_bus.element apply   # write; prints CHANGED<TAB><path>

Exit 78 on any refusal, with the reason on stderr.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import pathlib
import pwd
import stat
import sys
import urllib.parse
from collections.abc import Sequence
from dataclasses import dataclass

from helpers.agent_bus import teamfile
from helpers.pingbus import config, protocol

FLATPAK_APP = "im.riot.Riot"
PROFILE_PARENT = pathlib.PurePosixPath(config.ELEMENT_FLATPAK_DIR) / "config"
PROFILE_PREFIX = "Element-"
CONFIG_FILE = "config.json"
ELECTRON_STORE_FILE = "electron-config.json"
SPELLCHECK_SEED = {"spellCheckerEnabled": False}
APPLICATIONS_DIR = pathlib.PurePosixPath(".local/share/applications")

PROFILE_KEYS = frozenset({"team", "base_url", "server_name"})
REQUIRED_PROFILE_KEYS = ("team", "base_url")

DIR_MODE = 0o700
PRIVATE_MODE = 0o600
LAUNCHER_MODE = 0o644
CHANGED_MARKER = "CHANGED\t"


class ElementError(Exception):
    """Refused: the message names the team and key at fault, never a value."""

    EXIT_CODE = 78


@dataclass(frozen=True)
class Profile:
    team: str
    base_url: str
    server_name: str

    @property
    def host(self) -> str:
        """The homeserver's host, unbracketed: where an accidental Jitsi call stays."""
        return urllib.parse.urlsplit(self.base_url).hostname or ""


def _ip_hosts(value: str) -> tuple[str, ...]:
    """The URL's host when it is a canonical IP literal: plain HTTP is allowed only to
    an address (`<bus_ip>` or `<wg_ip>`, DESIGN.md section 3.3), never to a name."""
    try:
        host = urllib.parse.urlsplit(value).hostname
    except ValueError:
        return ()
    if not host or "%" in host:
        return ()
    try:
        return (str(ipaddress.ip_address(host)),)
    except ValueError:
        return ()


def _parse_one(position: int, data: object) -> Profile:
    if not isinstance(data, dict):
        raise ElementError(f"profile {position}: must be an object")
    if set(data) - PROFILE_KEYS:
        raise ElementError(f"profile {position}: the only keys are {', '.join(sorted(PROFILE_KEYS))}")
    for key in REQUIRED_PROFILE_KEYS:
        if key not in data:
            raise ElementError(f"profile {position}: {key} is required")
    team = data["team"]
    if not protocol.is_team_name(team):
        raise ElementError(f"profile {position}: team must match {protocol.TEAM_NAME_PATTERN}")

    def refuse(key: str, why: str) -> ElementError:
        return ElementError(f"team {team}: {key} {why}")

    base_url = data["base_url"]
    try:
        config.check_base_url(base_url, _ip_hosts(base_url) if isinstance(base_url, str) else (),
                              refuse)
    except ValueError:
        raise refuse("base_url", "is not a URL") from None
    try:
        server_name = teamfile.check_server_name(
            data.get("server_name", team + teamfile.SERVER_NAME_SUFFIX))
    except teamfile.TeamFileError as exc:
        raise ElementError(f"team {team}: {exc}") from None
    return Profile(team=team, base_url=base_url, server_name=server_name)


def parse_profiles(data: object) -> tuple[Profile, ...]:
    """Validate the decoded profile list; raise ElementError at the first broken rule."""
    if not isinstance(data, list):
        raise ElementError("profiles: must be a JSON array")
    profiles = tuple(_parse_one(position, entry) for position, entry in enumerate(data, start=1))
    teams = [p.team for p in profiles]
    if len(set(teams)) != len(teams):
        raise ElementError("profiles: a team is listed more than once")
    return profiles


def config_json(profile: Profile) -> dict:
    """The profile's `config.json`: the clients research §1.4, key for key."""
    return {
        "default_server_config": {
            "m.homeserver": {"base_url": profile.base_url, "server_name": profile.server_name},
        },
        "disable_custom_urls": True,
        "disable_guests": True,
        "disable_3pid_login": True,
        "disable_login_language_selector": True,
        "enable_client_well_known_lookups": False,
        "update_base_url": None,
        "integrations_ui_url": None,
        "integrations_rest_url": None,
        "integrations_widgets_urls": [],
        "bug_report_endpoint_url": None,
        "posthog": None,
        "sentry": None,
        "privacy_policy_url": None,
        "terms_and_conditions_links": [],
        "map_style_url": None,
        # An object default that cannot be nulled; pointed at the homeserver's host.
        "jitsi": {"preferred_domain": profile.host},
        "element_call": {"disable": True},
        "features": {
            "feature_video_rooms": False,
            "feature_group_calls": False,
            "feature_element_call_video_rooms": False,
        },
        "room_directory": {"servers": [profile.server_name]},
        "show_labs_settings": False,
        "mobile_guide_toast": False,
        "setting_defaults": {
            "UIFeature.urlPreviews": False,
            "UIFeature.voip": False,
            "UIFeature.widgets": False,
            "UIFeature.locationSharing": False,
            "UIFeature.identityServer": False,
            "UIFeature.thirdPartyId": False,
            "UIFeature.registration": False,
            "UIFeature.passwordReset": False,
            "UIFeature.deactivate": False,
            "UIFeature.feedback": False,
            "UIFeature.shareSocial": False,
            "UIFeature.allowCreatingPublicRooms": False,
            "UIFeature.allowCreatingPublicSpaces": False,
            "fallbackICEServerAllowed": False,
        },
    }


def launcher_text(profile: Profile) -> str:
    return (
        "[Desktop Entry]\n"
        "Type=Application\n"
        f"Name=Element ({profile.team} team bus)\n"
        f"Comment=Watch and command the {profile.team} agent team\n"
        f"Exec=flatpak run {FLATPAK_APP} --profile {profile.team}\n"
        f"Icon={FLATPAK_APP}\n"
        "Terminal=false\n"
        "Categories=Network;InstantMessaging;\n"
    )


def profile_dir(home: pathlib.Path, team: str) -> pathlib.Path:
    return home / PROFILE_PARENT / f"{PROFILE_PREFIX}{team}"


def launcher_path(home: pathlib.Path, team: str) -> pathlib.Path:
    return home / APPLICATIONS_DIR / f"agent-bus-{team}-element.desktop"


def pingbus_dir(home: pathlib.Path) -> pathlib.Path:
    return config.resolve_home({"HOME": str(home)})


def check_user(home: pathlib.Path) -> None:
    """Refuse a user who holds pingbus bundles. Anything at the path counts, a dangling
    symlink included; a path that cannot be looked at is a refusal, not a pass."""
    path = pingbus_dir(home)
    try:
        os.lstat(path)
    except FileNotFoundError:
        return
    except OSError as exc:
        raise ElementError(f"{path} cannot be checked for pingbus bundles ({exc.strerror})") from None
    raise ElementError(
        f"{path} exists: this user holds pingbus bundles, so no Element session may live "
        "beside them; configure Element for a human user, never an agent user"
    )


def _dumps(value: object) -> bytes:
    return (json.dumps(value, indent=2) + "\n").encode("utf-8")


def _ensure_dir(path: pathlib.Path, mode: int, changed: list[pathlib.Path]) -> None:
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        path.mkdir(mode=mode, parents=True)
        os.chmod(path, mode)
        return
    if not stat.S_ISDIR(st.st_mode):
        raise ElementError(f"{path} is not a directory")
    if stat.S_IMODE(st.st_mode) != mode:
        os.chmod(path, mode)
        changed.append(path)


def _write(path: pathlib.Path, content: bytes, mode: int, changed: list[pathlib.Path],
           *, only_if_absent: bool = False) -> None:
    """Write atomically, and only when the content or mode differs. A symlink or other
    non-regular file at the path is refused, never followed."""
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        st = None
    if st is not None:
        if not stat.S_ISREG(st.st_mode):
            raise ElementError(f"{path} is not a regular file")
        if only_if_absent:
            return
        if stat.S_IMODE(st.st_mode) == mode and path.read_bytes() == content:
            return
    tmp = path.with_name(f".{path.name}.tmp-{os.getpid()}")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        with os.fdopen(fd, "wb") as handle:
            os.fchmod(handle.fileno(), mode)
            handle.write(content)
        os.replace(tmp, path)
    except BaseException:
        tmp.unlink(missing_ok=True)
        raise
    changed.append(path)


def apply(home: pathlib.Path, profiles: Sequence[Profile]) -> list[pathlib.Path]:
    """Write every profile and launcher; return the paths changed, in order."""
    check_user(home)
    changed: list[pathlib.Path] = []
    for profile in profiles:
        pdir = profile_dir(home, profile.team)
        _ensure_dir(pdir, DIR_MODE, changed)
        _write(pdir / CONFIG_FILE, _dumps(config_json(profile)), PRIVATE_MODE, changed)
        _write(pdir / ELECTRON_STORE_FILE, _dumps(SPELLCHECK_SEED), PRIVATE_MODE, changed,
               only_if_absent=True)
        launcher = launcher_path(home, profile.team)
        launcher.parent.mkdir(parents=True, exist_ok=True)
        _write(launcher, launcher_text(profile).encode("utf-8"), LAUNCHER_MODE, changed)
    return changed


def _running_home() -> pathlib.Path:
    return pathlib.Path(pwd.getpwuid(os.geteuid()).pw_dir)


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python3 -m helpers.agent_bus.element")
    parser.add_argument("action", choices=("check", "apply"))
    args = parser.parse_args(argv)
    try:
        if os.geteuid() == 0:
            raise ElementError("run as the human's desktop user, not root")
        home = _running_home()
        try:
            data = teamfile.decode_strict_json(sys.stdin.read())
        except ValueError as exc:
            raise ElementError(f"profiles: not valid JSON ({exc})") from None
        profiles = parse_profiles(data)
        if args.action == "check":
            check_user(home)
            return 0
        for path in apply(home, profiles):
            print(f"{CHANGED_MARKER}{path}")
    except ElementError as exc:
        print(f"agent-bus element: {exc}", file=sys.stderr)
        return ElementError.EXIT_CODE
    return 0


if __name__ == "__main__":
    sys.exit(main())
