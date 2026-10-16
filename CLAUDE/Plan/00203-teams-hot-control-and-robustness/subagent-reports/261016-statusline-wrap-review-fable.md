# Review of 79475c19 (`fix/bus-statusline-wrap`): 👥 teams on rows of their own — Fable

**Verdict: FIX-FIRST** (one blocker, small; the code itself is sound).

Reviewed against local F44 (`1d3c65bb`). Branch base `0976d613`; F44 has four commits the
branch lacks (the coordinator-two-paths merge), none touching `helpers/pingbus` or its tests.

## Blocker

**B1. Plan 00201's acceptance still asserts the old `| bus: TEAMS` shape and fails on this
code.** Plan 00201 is `In Progress` (its PLAN.md:3 says what remains is the owner's steps in
ACCEPTANCE-owner.md), and this commit changes the very output its M3 leg and owner steps check:

- `CLAUDE/Plan/00201-…/m7_check.py:489-495` — `expect_segment` takes the LAST line, splits on
  `" | "` and requires `segment == f"bus: {teams}"`. The last line is now `👥 TEAMS` with no
  `" | "`, so this check raises `Failed` on every run.
- `CLAUDE/Plan/00201-…/test_m7_check.py:367-372` — fixtures `… | \x1b[36mbus: a,b\x1b[0m`.
- `CLAUDE/Plan/00201-…/acceptance.bash:40,168` and `_acceptance-m7.inc.bash:639,656` —
  leg descriptions/evidence text `bus: ${TEAM_A},${TEAM_B}`.
- `CLAUDE/Plan/00201-…/ACCEPTANCE-owner.md:50,67,94-95,106` — the owner's PENDING manual steps
  tell the owner to look for `bus: <team>` / `bus: A,B` and treat red `bus: no record` as the
  fault. These are the steps the plan is waiting on; they now describe output that no longer
  exists.
- Secondary (spec wording, same plan): `SPEC-many-teams.md:25,179,183`, `PLAN.md:91,127,132`.

Fix: `expect_segment` should take the last line (ANSI stripped) and require it to equal
`👥 {teams}` (and `_ANSI_RE` there already strips SGR); update the fixtures and the owner steps
to `👥`. The 00203 side (`_acceptance-m3.inc.bash:28,235`, ACCEPTANCE.md, SPEC) was updated
correctly in this commit; 00201 was missed.

## Attack results

### 1. Width measurement

`display_width` (statusline.py:290-298) is a line-for-line copy of the daemon's
`_display_width` (`.claude/hooks-daemon/src/claude_code_hooks_daemon/core/hook_result.py:155-173`,
same `\x1b\[[0-9;]*[A-Za-z]` regex, same combining/Cf skip, same W/F → 2). The claim "as the
daemon measures it" is literally true.

Probed values: ZWJ family 6 (string-width: 2), flag 2, `❤️` 2, CJK `漢字` 4, `a\tb` 3, OSC 8
hyperlink counted at full literal length (26), CSI `?25l` not stripped (7), keycap `1️⃣` 3,
`👥` 2, `👤` 2. So against Claude Code's string-width it over-counts ZWJ/keycap sequences
(wraps early, never cuts) and mis-counts tabs, OSC 8 and non-SGR CSI (could under-count).

**None of this is reachable.** The text measured is only: the label `👥` (W → 2, same in
string-width), team names (`TEAM_NAME_RE = [a-z][a-z0-9-]{0,23}`), the ASCII suffix `(down)`,
the two ASCII notes, and the module's own SGR codes. The inner status line (where the daemon's
OSC 8 or other sequences would live) is passed through and never measured. The truncation
cannot return through a measurement mismatch. Non-blocker N1 below suggests pinning this.

### 2. Fallback paths

`COLUMNS` unset/non-positive → one unbroken row below the inner. This is a stated decision
(module docstring, `docs/agent-bus.md:601-602`, `docs/agent-bus-protocol.md:751`, changelog)
and tested (`test_an_unknown_width_puts_the_segment_on_one_row_of_its_own`, six variants
including `²`). Not a hidden fallback. Evidence Claude Code sets COLUMNS for status line
commands: the daemon's transport comment at `.claude/hooks-daemon/init.sh:3313-3315`
("older Claude Code clients (\<2.1.153, which sends no COLUMNS)") and `input_schemas.py:292-296`;
the installed Claude Code is 2.1.301. The M3 acceptance invokes `pingbus status-line` through
`podman exec` with no COLUMNS, so it exercises the one-row path on purpose and its substring
match still works. The owner should still decide whether "one row when COLUMNS is absent" is
the single correct path they want; the alternative (a red note) would hide the teams for no gain.

### 3. Inner output

Unchanged from the base apart from the joiner (`" | "` → `"\n"`). Probed: empty inner →
segment alone; `"\n\n"` → segment alone (trailing newlines were already `rstrip`ped before this
commit); interior blank lines, leading/trailing spaces, tabs and OSC 8 pass through byte for
byte; non-zero exit → text + red `(status line exit N)` then the segment on its own row; the
command still always exits 0 (Claude Code blanks the line otherwise). Interior `\n` in the
inner is not re-measured or re-wrapped — correct, the daemon already wrapped it.

### 4. Earlier fixes

The cancel-during-start code (`_starting`, `_cancel_pending`, `run_command`'s `try/finally`,
`_cancelled`) is byte-identical to the base: `git diff 0976d613 HEAD -- helpers/pingbus/statusline.py`
touches neither name. Tests for it still pass.

### 5. Leftover labels

Plan 00203, docs/, helpers/, tests/: none (the `bus:` hits remaining are `agent team bus:` /
`agent-bus:` prose, D-Bus, and changelog history). Plan 00201: see B1. M3 acceptance
(`_acceptance-m3.inc.bash:235`) matches `*"👥 ${M3_TEAM_A},${M3_TEAM_B}"*` against the raw
output `\e[36m👥 acc-m3a,acc-m3b\e[0m` — works (no COLUMNS under `podman exec`, so one row).

### 6. Public repo

No hostnames, usernames, container names or checkout paths in the diff. Team names are the
plan's throwaway `acc-m3a`/`acc-m3b` already in the repo.

### 7. Tests

`python3 -m pytest tests/helpers/pingbus/test_statusline.py` with `CCY_SESSION_NAME` and every
`PINGBUS_*` unset: 44 passed.

### 8. Merge dry run

`git merge-tree --write-tree F44 HEAD` → conflicts in exactly the four renumber files:
`CLAUDE/Plan/meta-deploy.list`, `docs/ccy-changelog.md`, `files/var/local/claude-yolo/Dockerfile`,
`files/var/local/claude-yolo/claude-yolo`. F44 is at CCY 3.99.12.4 / container 2.77.3; the
branch at placeholders 3.99.12.7 / 2.77.6 (launcher `CCY_VERSION` line 17 and
`REQUIRED_CONTAINER_VERSION` line 81, Dockerfile LABEL line 36 all agree). No conflicts in
code, tests, or the 00203 plan files (ACCEPTANCE.md, PLAN.md, SPEC auto-merge).

## Non-blockers

- **N1.** Add a test (or assertion) that the measured text is ASCII plus `LABEL`: the width
  guarantee rests on `TEAM_NAME_RE` and the two ASCII constants, and nothing says so where
  `display_width` is defined. A future `(down)` → `⚠` change would silently widen the gap with
  string-width.
- **N2.** `ANSI_ESCAPE_RE` is named "A colour (SGR/CSI) escape" but matches only CSI with
  `[0-9;]` parameters; fine for the module's own output, but the docstring of `display_width`
  should say the input is this module's own text, not arbitrary terminal output.
- **N3.** `_note` splits on single spaces; both notes are single-spaced, so fine — a note with
  two spaces would produce an empty piece and a stray space. Not reachable today.
- **N4.** `docs/ccy-changelog.md:23-27` and `meta-deploy.list` entry are correct; at renumber
  remember the changelog heading and the `meta-deploy.list` comment both carry the placeholder.
