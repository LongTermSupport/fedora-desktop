# SPEC: team colours in the status line (Plan 00217)

Each agent team the session is on is drawn in its own colour on the status line's `👥` rows, so
the owner can tell at a glance which team or teams a session is on. Every team now shows in the
same cyan.

This extends Plan 00213's [SPEC-status-line.md](../00213-agent-bus-performance-go-carve-out/SPEC-status-line.md)
S26 to S30 (the segment's text and wrapping) and S33 (the cache). Every other expectation there
holds unchanged. Both implementations, `helpers/pingbus/statusline.py` and
`src-go/internal/pingbus/statusline`, give the same bytes; the parity oracle is re-recorded from
the Python (SPEC-status-line S40).

## 1. The palette (`helpers/pingbus/colours.py`, its Go copy `statusline/colour.go`)

- **C1** The palette is ten named 24-bit colours, in this order:

  | Index | Name    | Hex       |
  | ----- | ------- | --------- |
  | 0     | teal    | `#2dd4bf` |
  | 1     | cyan    | `#22d3ee` |
  | 2     | sky     | `#7dd3fc` |
  | 3     | blue    | `#60a5fa` |
  | 4     | indigo  | `#a5b4fc` |
  | 5     | violet  | `#a78bfa` |
  | 6     | purple  | `#d8b4fe` |
  | 7     | magenta | `#e879f9` |
  | 8     | pink    | `#f9a8d4` |
  | 9     | mint    | `#99f6e4` |

- **C2** **NOT BOILERPLATE: legible on a dark terminal.** Every palette colour has a WCAG 2.x
  contrast ratio of at least 4.5:1 (AA for normal text) against each reference dark background:
  `#000000`, `#1e1e1e`, `#242424` (the GNOME and Ptyxis dark default), `#282c34` and `#2e3440`.
  A test in each language computes it (relative luminance from linearised sRGB) and fails on any
  colour below. The lowest is violet against `#2e3440`, about 4.6:1.

- **C3** **No hue that already means a state.** The status line's other segments use red for
  alerts and notes (pingbus's own notes are `ESC[31m`), orange, yellow and green for the hooks
  daemon's context tiers and model, so every palette hue (HSV) lies from 160° to 335°: cyan to
  pink, through blue and violet. A test pins the band.

- **C4** The colours are told apart: every pair is at least 15 apart in CIE76 ΔE (CIELAB, D65). A
  test pins it. The closest pair is blue and indigo, about 16.

- **C5** **Auto colour.** A team's automatic colour is `palette[fnv1a32(name) % 10]`: 32-bit FNV-1a
  over the name's bytes (offset basis `0x811c9dc5`, prime `0x01000193`). It depends on the name
  alone, so a team has the same colour in every session and on every machine, and never changes
  from one render to the next. Two teams can share one; an override (C7) separates them.

- **C6** The Go palette and hash are a copy of the Python's: a Go test runs the Python module from
  the checkout and requires the same names, hexes and auto colour for a sample of team names.

## 2. The override

- **C7** A present `agent_bus_teams` entry may set `colour:`, a palette name (C1, lower case) or
  `#rrggbb` (six hex digits, either case, stored lower case). It is validated by the team file's
  rules (`teamfile.parse_team_file`), so `ccy-teams`, `declare check` and the play all refuse the
  same values, with `team file: colour: ...` naming the rule:
  - not a string, or neither a palette name nor `#` and six hex digits: `must be one of <names>, or #rrggbb`;
  - a hex below C2's contrast against any reference background: `#<hex> is too dark to read on a dark terminal (contrast <ratio>:1 against <background>, at least 4.5:1 needed)`.
    An entry without `colour` uses the auto colour.
- **C8** **It travels as room state, never in the team record.** The record is the team's trust
  record, and a member that does not know a key refuses the whole record and stops the team; the
  description goes as the room topic for the same reason. The admin (`agent_bus.admin`'s
  bootstrap, run by every `agent-bus-install team`) keeps the main room's `agent_bus.colour` state
  event (key `""`) at `{"colour": "#rrggbb"}`, the entry's colour as hex, or at `{}` when the entry
  sets none and the event exists; it writes only when that differs, and reports `colour`. The room's
  `state_default` of 100 lets only the admin write it. A changed colour restarts no homeserver: the
  team file's `team.json` is not a restart input (`agent-bus-install`).
- **C9** A member's syncer reads the event from the admin alone, when it verifies the room and when
  a sync carries that event type (both filters name it). An event from anyone else, or one whose
  content is not exactly `{"colour": <a colour C7 accepts as hex>}`, is no colour and never a loss
  of trust (the event is informational, as the topic is), and is said once in the watcher's log.
  The colour is kept in `<bundle>/state/colour.json` as `{"v": 1, "colour": "#rrggbb"}`, mode
  0600, written atomically; with no colour the file is removed. A loss of trust removes it too.
  A member on an older pingbus ignores the event type: it shows the auto colour, and nothing else
  changes for it.

## 3. The status line

- **C10** **A colour span per team.** A team row is the label (or the indent), then each team's
  piece (`<team> (<role>)` and its marks, with its comma) in its own span,
  `ESC[38;2;<r>;<g>;<b>m<piece>ESC[0m` (decimal components), the pieces joined by one plain
  space. The label `👥`, the indent and the joining spaces carry no colour. Wrapping (S27 to S29)
  is unchanged: escapes take no columns, and rows break only between pieces.
- **C11** The red notes (S17, S19, S30) are unchanged: one `ESC[31m` span per row, label included.
- **C12** **Reading the override.** For each team shown, `<home>/<team>/state/colour.json`, with
  the home of S20. No home: every team is auto, and S20's diagnostic covers it. It is opened with
  O_NOFOLLOW|O_NONBLOCK, must be a regular file of at most 4096 bytes, is decoded as UTF-8 JSON,
  and must be exactly `{"v": 1, "colour": "#rrggbb"}` (lower-case hex; any `v` other than the
  integer 1 is refused). It is read whether or not the team's role could be read.
  - absent (ENOENT): the auto colour, nothing said;
  - any other failure: the auto colour, the mark ` (colour?)` after the team's other marks, and
    one stderr line for every such team of the render:
    `pingbus status-line: the colour cannot be read (<team>: <reason>; <team>: <reason>)`, after
    the role line when there is one. The reasons, the same bytes in both implementations:
    `the colour file is a symlink`, `the colour file is not a regular file`,
    `the colour file is too large`, `the colour file cannot be read`,
    `the colour file is not JSON`, `the colour file is not {"v": 1, "colour": "#rrggbb"}`.
    The contrast of a stored hex is not judged again here: C9 judged it before writing.
- **C13** The cache (S33) fingerprints each `colour.json` read or looked for, so a colour that
  changes, appears or goes is drawn at the next render; the colour stderr line is stored and
  printed again with the role line (S35).
- **C14** 24-bit colour only. The status line has never told 16, 256 and 24-bit terminals apart
  (it writes `ESC[36m` and `ESC[31m` to every terminal), so it gains no 256-colour fallback;
  tmux and the GNOME terminals map 24-bit colour themselves.

## 4. Not built

- **A light-background palette.** Future work only: a second palette judged against light
  references, chosen by a setting or by the terminal's reported background (OSC 11). Not built,
  and nothing reads such a setting yet.
- **v2 (Plan 00209).** v2's status line reads `status.json` from the link (DESIGN-v2-B-member
  2.9) and its team config is `teamchat_teams[]`; it takes the palette, the hash and the
  override grammar as they are here, and its `status.json` `teams[]` gains a `colour`. Recorded as
  a follow-up of this plan for 00209 to pick up; 00209's files are not changed here.
