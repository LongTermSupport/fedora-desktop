"""The agent teams' colours on the status line (Plan 00217, SPEC-team-colours.md C1 to C7).

Each team row piece is drawn in its team's colour: the override its `agent_bus_teams` entry sets
(`colour:`, carried to members as the admin's `agent_bus.colour` room state), else the automatic
one, `PALETTE[fnv1a32(name) % len(PALETTE)]`, which depends on the name alone and so is the same
in every session and on every machine.

The palette is legible on a dark terminal (WCAG AA, 4.5:1, against each of DARK_BACKGROUNDS) and
keeps out of the hues the status line already gives a meaning (red notes and alerts, the hooks
daemon's orange, yellow and green tiers): its tests hold both. A light-background palette is
future work (the SPEC's section 4).

A leaf module of the standard library alone: the status line imports it on every render, the
team file and the syncer to judge an override. `src-go/internal/pingbus/statusline/colour.go`
is its Go copy, which a Go test keeps equal to this one.
"""

from __future__ import annotations

import re

PALETTE = (
    ("teal", "#2dd4bf"),
    ("cyan", "#22d3ee"),
    ("sky", "#7dd3fc"),
    ("blue", "#60a5fa"),
    ("indigo", "#a5b4fc"),
    ("violet", "#a78bfa"),
    ("purple", "#d8b4fe"),
    ("magenta", "#e879f9"),
    ("pink", "#f9a8d4"),
    ("mint", "#99f6e4"),
)
#: Typical dark terminal backgrounds: black, two editor darks, the GNOME and Ptyxis default
#: (#242424) and Nord. Every colour, palette or override, must reach MIN_CONTRAST on each.
DARK_BACKGROUNDS = ("#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440")
MIN_CONTRAST = 4.5
FNV_OFFSET = 0x811C9DC5
FNV_PRIME = 0x01000193
_HEX_RE = re.compile(r"#[0-9A-Fa-f]{6}")
_STORED_RE = re.compile(r"#[0-9a-f]{6}")
_BY_NAME = dict(PALETTE)


class ColourError(ValueError):
    """A colour override that is not a palette name or a legible #rrggbb; the message says why."""


def fnv1a32(data: bytes) -> int:
    """32-bit FNV-1a of `data`."""
    value = FNV_OFFSET
    for byte in data:
        value = ((value ^ byte) * FNV_PRIME) & 0xFFFFFFFF
    return value


def auto_colour(team: str) -> str:
    """The team's automatic colour, as #rrggbb."""
    return PALETTE[fnv1a32(team.encode("utf-8")) % len(PALETTE)][1]


def linear(channel: int) -> float:
    """An sRGB channel (0 to 255) linearised, as WCAG's relative luminance takes it."""
    c = channel / 255
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def _luminance(hex_colour: str) -> float:
    r, g, b = (linear(int(hex_colour[i:i + 2], 16)) for i in (1, 3, 5))
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast_ratio(first: str, second: str) -> float:
    """WCAG 2.x contrast ratio of two #rrggbb colours, 1 to 21."""
    lighter, darker = sorted((_luminance(first), _luminance(second)), reverse=True)
    return (lighter + 0.05) / (darker + 0.05)


def parse_colour(value: object) -> str:
    """An override as stored, lower-case #rrggbb: a palette name's colour, or a hex legible on
    every dark background. ColourError naming the rule otherwise."""
    if isinstance(value, str) and value in _BY_NAME:
        return _BY_NAME[value]
    if not isinstance(value, str) or not _HEX_RE.fullmatch(value):
        names = ", ".join(name for name, _ in PALETTE)
        raise ColourError(f"must be one of {names}, or #rrggbb")
    hex_colour = value.lower()
    ratio, background = min((contrast_ratio(hex_colour, bg), bg) for bg in DARK_BACKGROUNDS)
    if ratio < MIN_CONTRAST:
        raise ColourError(f"{hex_colour} is too dark to read on a dark terminal (contrast {ratio:.2f}:1 "
                          f"against {background}, at least {MIN_CONTRAST:g}:1 needed)")
    return hex_colour


def is_stored_colour(value: object) -> bool:
    """Whether `value` is a colour as stored: lower-case #rrggbb."""
    return isinstance(value, str) and _STORED_RE.fullmatch(value) is not None


def sgr(hex_colour: str) -> str:
    """The 24-bit foreground escape of a #rrggbb colour."""
    r, g, b = (int(hex_colour[i:i + 2], 16) for i in (1, 3, 5))
    return f"\033[38;2;{r};{g};{b}m"
