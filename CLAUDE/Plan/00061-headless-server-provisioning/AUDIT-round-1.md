# Audit — Round 1 (Fable, adversarial auditor)

**Method**: every claim below was checked against the real files in this repo
(`playbooks/**`, `scripts/qa-ansible.bash`, `scripts/qa-all.bash`,
`docs/playbooks.md`, `CLAUDE/AnsibleStyle.md`), and the two highest-risk
mechanical claims (the awk/grep parser, the `qa-all.bash` merge) were
**executed**, not just read — see §1 for the exact repro commands and their
real output. I did not re-litigate the tags+`--skip-tags` mechanism itself
(locked per the owner's decision) or re-derive scope classifications from
scratch; I verified the proposal's classifications and code against the repo
as it exists today.

---

## 1. The QA gate code (§4 of PROPOSAL.md) — **1 BLOCKER, 2 SHOULD-FIX**

### BLOCKER 1.1 — `valid_count=$(... | grep -xcE ...)` crashes the whole script under `set -e`, and silently corrupts `qa-all.bash`'s merged JSON for every other check

**Claim being tested**: PROPOSAL.md lines ~430–431:

```bash
valid_count=$(printf '%s\n' "$play_tags" | grep -xcE "$SCOPE_VALID_RE")
scope_like_count=$(printf '%s\n' "$play_tags" | grep -xcE 'scope-[A-Za-z0-9_-]+')
```

**Why it's wrong**: `scripts/qa-ansible.bash` opens with `set -euo pipefail`.
`grep -c` exits **1** (not 0) when it finds zero matching lines — it still
prints `0` to stdout, but its own exit status is non-zero. A bash simple
command of the form `var=$(pipeline)` takes the pipeline's exit status as its
own; under `set -e` a non-zero exit status on *any* simple command (outside an
`if`/`while` condition or an `&&`/`||` chain) aborts the script immediately.
**This is exactly the case Check 4 exists to catch** — a play with zero valid
scope tags — so the check crashes precisely when it's supposed to fire. On
day one, before any play is tagged, this fires on the **very first** file
`find` hands the loop.

I built the exact snippet from the proposal and ran it under `set -euo pipefail`:

```bash
$ ./test-scope-check.bash
BEFORE valid_count
$ echo $?
1
```

`AFTER valid_count` never prints — the script dies mid-statement, before the
`if/elif` chain that would produce the nice `ERROR (scope): ... — no play-level scope tag` message ever runs. **The check's own designed failure
mode never executes.**

**It gets worse — this corrupts `qa-all.bash`'s output for unrelated checks,
not just this one.** `qa-ansible.bash`'s Check 4 sits *before* the "Build JSON
output" section (confirmed — see §1.3 below), so when it crashes,
`jq -n ... > "$JSON_OUT"` never runs and `$TMP_ANSIBLE` (the file
`qa-all.bash` passes as `QA_JSON_OUT`) stays **completely empty**.
`qa-all.bash`'s final merge is:

```bash
jq -s ... "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" > "$JSON_OUT"
```

I tested `jq -s` with an empty file in the middle of the argument list:

```bash
$ jq -s '.' f1.json f2-empty.json f3.json
[
  { "a": 1 },
  { "a": 3 }
]
$ echo $?
0
```

`jq -s` silently **drops** the empty input and produces a **shorter array**
— exit code 0, no warning. Applied to `qa-all.bash`, this means the
positional merge `checks.ansible: .[3]` would actually receive
`qa-ansible-syntax`'s JSON (the array shifts left by one), and
`checks.js: .[5]` would go out of bounds (`null`). This is not confined to
losing the scope report — it corrupts the `checks` object for **every**
check after the crash point, silently, with exit 0 from the merge step.
`qa-all.bash`'s own top-level `✓`/`✗` terse verdict would still correctly say
FAILED (it captures `qa-ansible.bash`'s crash exit code via `|| rc=$?`
outside the JSON), but the machine-readable `/tmp/qa-results.json` that
`CLAUDE/QA.md` tells contributors to inspect via `jq '.failures[]'` would be
silently wrong — exactly the failure mode this repo's stderr-hygiene /
fail-fast culture is built to prevent.

**Confirmed this is not a pattern this proposal merely inherited**: the
*existing* checks 1 and 3 in the same file already defend against this exact
hazard —
`grep -rni ... > "$TMP_MATCHES" 2>"$TMP_GREP_ERR" || grep_rc=$?` and
`grep -rnP ... > "$TMP_MATCHES" 2>"$TMP_GREP_ERR" || selfref_rc=$?` both use
the `|| rc=$?` guard specifically so a "no matches" grep doesn't trip
`set -e`. Check 4's `valid_count=$(...)`/`scope_like_count=$(...)` lines are
new code that skips this established, load-bearing guard.

**Concrete fix** (verified empirically to both avoid the crash and preserve
the correct count):

```bash
valid_count=$(printf '%s\n' "$play_tags" | grep -xcE "$SCOPE_VALID_RE" || true)
scope_like_count=$(printf '%s\n' "$play_tags" | grep -xcE 'scope-[A-Za-z0-9_-]+' || true)
```

I re-ran the exact snippet with this fix, under `set -euo pipefail`, for both
a zero-match case and a one-match case:

```
valid_count: 0
scope_like_count: 0
valid_count2 (should be 1): 1
Reached end of script successfully
SCRIPT EXIT CODE: 0
```

Confirms the fix is sufficient and doesn't change the semantics the proposal
already designed (0/1/2+ branches all still work).

### SHOULD-FIX 1.2 — trailing `#` comments on a scope tag line silently defeat the parser (conflicts with this repo's own comment convention)

`grep -xcE` (`-x` = whole-line exact match) is used against each extracted
item. The awk extraction only strips the `   -` prefix:

```awk
sub(/^    - [[:space:]]*/, "")
```

It does **not** strip a trailing comment. If a contributor writes (a
completely natural thing to do, and explicitly encouraged by this repo's own
"Comments explain WHY not what" style rule):

```yaml
  tags:
    - scope-gnome  # needs a live GNOME session for gsettings/dconf
```

the extracted value becomes `scope-gnome  # needs a live GNOME session...`.
`grep -xcE "$SCOPE_VALID_RE"` does not match it (not an exact match), **and**
`grep -xcE 'scope-[A-Za-z0-9_-]+'` (the "scope-like but invalid" detector)
*also* doesn't match it (same exact-match problem) — so `scope_like_count`
is 0 too, and the gate reports **"no play-level scope tag"**, which is
actively misleading: a tag is present, correctly spelled, just commented.
This will be the very first thing a well-intentioned contributor hits.

**Fix**: strip a trailing comment in the same awk pass:

```awk
in_block && /^    - [[:space:]]*[^[:space:]]/ {
    sub(/^    - [[:space:]]*/, "")
    sub(/[[:space:]]*#.*$/, "")
    print
    next
}
```

### Verified correct (no finding) — parser tracing against real files

I traced the awk parser by hand against three real files and confirmed each
behaves as the proposal claims:

- **`playbooks/imports/play-gsettings.yml`** — no `become:`, no `vars:` at
  play level (verified by reading the file); confirms the proposal's claim
  that key position doesn't matter to either Ansible or the parser (the
  parser only anchors on `^  tags:$` at 2-space indent, not on adjacency to
  any other key).
- **`playbooks/imports/play-python.yml`** — has a real task-level `tags:`
  block (`pyenv`, `pyenv_install_versions`) at 6-space indent (task
  properties align two levels deeper than play-level keys). `^  tags:` (2
  literal spaces) does not match a 6-space-indented line — confirmed the
  parser correctly ignores it. Same holds for `play-terminal-emulators.yml`'s
  scalar-form task tags (`tags: alacritty`), which are also indented past
  2 spaces.
- **`playbooks/imports/play-podman.yml`** — play-level keys (`name:`,
  `become:`, `vars:`, `tasks:`) confirmed at exactly 2-space indent in the
  real file, matching what the proposed `tags:` insertion needs to align
  with.

I also checked the specific edge cases the audit brief asked about:

- **Empty `tags:` block** (a `tags:` line with no following `- ` items):
  `in_block` is set, the very next line fails the item regex, `exit` fires,
  `play_tags` is empty. `printf '%s\n' "" | grep -xcE ...` counts 0 (verified
  — see §1.1's repro) — correctly falls into "missing scope tag", not a
  crash-free path exactly because of the same bug in 1.1. Once 1.1 is fixed,
  this resolves correctly (fails closed, as intended).
- **`tags:` block as the last thing in the file** (no trailing content after
  the last item): the `in_block { exit }` branch is only needed to stop
  *early*; hitting EOF while still `in_block` ends the awk program with the
  same result either way. No bug.
- **CRLF line endings**: checked with `cat -A` on
  `play-prevent-ssh-suspend.yml` (chosen because it's one of the three files
  this proposal edits) — confirmed **LF only**, no `^M`. Not a live risk in
  this repo today, but worth naming: if CRLF ever crept in (this repo's own
  `play-git-configure-and-tools.yml` sets `core.autocrlf input` specifically
  because it's aware of this class of risk), a trailing `\r` on an extracted
  item would defeat the same `-x` exact-match the comment issue above does,
  for the same underlying reason. Not filing as a separate finding — the
  fix for 1.2 (strip trailing junk after the value) also happens to reduce
  exposure here if someone wants to add `sub(/\r$/, "")` at the same time,
  but I'm not asserting this is currently exploitable.
- **Tabs**: YAML forbids literal tabs for indentation, and this repo's other
  QA gates (`qa-ansible-syntax.bash`'s `--syntax-check`) would already reject
  a tab-indented playbook before Check 4 ever saw it. Not a real exposure.
- **A file with multiple `- hosts:` play blocks** — see BLOCKER... actually
  filed as SHOULD-FIX 5.1 below, because I found a **real instance** of this
  in the repo the proposal's own classification table doesn't account for.
- **Inline `tags: [scope-general]` array form** — proposal documents this as
  a known, accepted limitation (§8). Confirmed by inspection that the parser
  only recognises block-list form (`sub(/^  tags:[[:space:]]*$/...)` requires
  the line to be *just* `  tags:`, which an inline array form on the same
  line would never be — `  tags: [scope-general]` would fail to match
  `in_block` at all, correctly triggering "missing scope tag," fail-closed).
  Accurately documented, not a bug.

---

## 2. `qa-all.bash` zero-changes claim — **verified accurate, no finding**

Read the real `scripts/qa-all.bash`. Confirmed:

- `QA_JSON_OUT="$TMP_ANSIBLE" "$SCRIPT_DIR/qa-ansible.bash" || rc=$?` captures
  `qa-ansible.bash`'s one JSON blob into `$TMP_ANSIBLE`.
- The final `jq -s ... '{ ... "checks": { ..., "ansible": .[3], ... } }'`
  merge takes `$TMP_ANSIBLE` as its 4th positional file argument (matching
  `.[3]`) — unchanged by this proposal.
- `qa-all.bash`'s own pass/fail (`FAILED=$((FAILED + 1))`, top-level
  `summary.failed`/`summary.total`) is driven by `qa-ansible.bash`'s **exit
  code** and its JSON's `summary.total/passed/failed`, both of which are
  computed from the shared `$ERRORS` counter that Check 4 increments
  identically to Checks 1–3. **Provided Blocker 1.1 is fixed**, a scope
  violation genuinely propagates through unchanged: `ERRORS` rises →
  `qa-ansible.bash`'s own `STATUS`/exit code flips to fail → `qa-all.bash`'s
  `FAILED` counter increments → top-level `✗ QA FAILED` and
  `jq '.failures[]' /tmp/qa-results.json` genuinely includes the new
  `"error": "scope: ..."` entries (`$sc` is correctly folded into the
  `failures` concatenation and the `checks.ansible.scope` key in the exact
  `jq -n` snippet shown in §4). The "zero `qa-all.bash` edits" claim is
  **correct** — conditional on 1.1's fix, since an unfixed Check 4 never
  reaches the `jq -n` call at all.

One cosmetic-only observation, not filed as a finding: \`TOTAL=$((PLAYBOOK_COUNT

- 1))`is not extended with a synthetic`+1`for the new check, unlike the existing "+1 per check" idiom the file uses for Check 1. This has zero effect on pass/fail (that's driven by`$ERRORS\`), so it's a nitpick (§ below), not a
  correctness issue.

---

## 3. The three mixed-play edits — **verified verbatim, claims accurate**

All three "Before" blocks in §3.1–§3.3 were diffed against the real files by
reading them directly:

- **`play-basic-configs.yml`** — "Deploy USB audio fix script" task (lines
  183–192 in the real file) matches the proposal's quoted block **verbatim**,
  field-for-field, including the `owner`/`group` Jinja expression and the
  `loop:` list.
- **`play-prevent-ssh-suspend.yml`** — read the full file (only 8 tasks). The
  quoted "Disable suspend on AC power" task matches verbatim, including
  `become_user`, the `argv:` list, the `environment:` block, and
  `changed_when: false`. **Confirmed the "hard-fails and aborts the whole
  play" claim is accurate**: this task has `changed_when: false` and **no**
  `failed_when` anywhere on it. `ansible.builtin.command` fails on any
  non-zero return code by default when `failed_when` is absent, and
  `gsettings set` on a schema that isn't registered (no
  `gnome-settings-daemon` on a headless box) returns non-zero and prints "No
  such schema" to stderr — standard, well-known `gsettings` behaviour. With
  no `ignore_errors`/`failed_when: false` on the task and no
  `any_errors_fatal`-style suppression in `ansible.cfg` (checked — `ansible.cfg`
  actually sets `any_errors_fatal = true`, which makes a single host failure
  abort the **entire play run**, not just the current play, an even stronger
  version of the proposal's claim), this is a real, verified latent bug that
  the scope split genuinely fixes, not an overstated risk.
- **`play-vpn.yml`** — read the full file (27 lines, only 2 tasks). The
  "Before" 3-package `dnf` task matches verbatim. The proposed split is
  **functionally identical on desktop** (all 3 packages still installed, the
  `firewalld` task after it is untouched) with one minor, non-functional
  difference worth a one-line caveat: splitting one `dnf` task into two means
  Ansible now reports **two independent `changed`/`ok` results** instead of
  one combined result in `-v`/check-mode output and the play recap — purely
  cosmetic, not a behavioural regression, but the proposal doesn't mention it
  (nitpick, filed below).

---

## 4. `--list-tasks` safety + zero-regression proof — **verified accurate by actually running it**

This container has `ansible-core 2.19.11` installed
(`/root/.local/bin/ansible-playbook`). I ran the exact commands from §6
against the **unmodified** repo (no tags added yet, so this tests the
*mechanism*, not the post-implementation diff):

```
$ ansible-playbook playbooks/playbook-main.yml --list-tasks
EXIT: 0        (439 lines of output; no host connection, no fact-gathering)

$ ansible-playbook playbooks/playbook-main.yml --skip-tags scope-server --list-tasks
$ diff list-before.txt list-skip-nonexistent.txt
(empty diff, exit 0)
```

This directly confirms the two central claims of §6:

1. **`--list-tasks` is genuinely container-safe** — it ran cleanly inside
   this CCY container against `localhost`/local transport with no error, no
   attempt to gather facts from a "real" target. The proposal's correction of
   `brainstorm-sonnet.md`'s `--check` claim is right — `--check` does gather
   facts and touch the target; `--list-tasks` does not.
2. **Skipping a tag that no task carries produces a byte-identical diff** —
   proving the "desktop command is a no-op today" claim is not just
   plausible, it's demonstrated, mechanically, on the actual repo.

I additionally ran `--skip-tags dnf` (an existing, real task-level tag) as a
sanity check that `--skip-tags` genuinely removes matching tasks from
`--list-tasks` output (not just a no-op flag): it correctly dropped all 16
`[dnf, upgrade]`-tagged tasks from `play-AB-dnf-upgrade.yml` while leaving
everything else untouched. This is strong secondary evidence the mechanism
will behave as documented once real `scope-gnome` tags are applied.

No finding here — this section is solid.

---

## 5. Classification correctness spot-check — **1 SHOULD-FIX (real 4th mixed-play-shaped issue, different in kind from the other three), 0 additional mixed CORE plays found**

### SHOULD-FIX 5.1 — `play-virtualbox-windows.yml` has TWO `- hosts:` play blocks in one file; the proposal's classification (and the QA gate's file-granularity design) doesn't account for this

```
$ grep -n '^-[[:space:]]*hosts:\|^  name:' playbooks/imports/optional/experimental/play-virtualbox-windows.yml
3:- hosts: desktop
4:  name: Install Virtualbox
44:- hosts: desktop
45:  name: Setup Windows VMs
```

This is a real, verified instance of exactly the edge case the audit brief
asked me to check ("a play with multiple `- hosts:` docs in one file"). It
breaks two assumptions simultaneously:

- **The proposal's own classification table** (§1.3) lists this file as
  **one row** with **one** scope value (`scope-gnome`, flagged Low
  confidence for the owner, same category as the rpm-fusion call) — but it
  is actually two independent plays ("Install Virtualbox" — driver +
  packages + group membership, arguably headless-capable via
  `VBoxHeadless`/`VBoxManage` per the proposal's own reasoning elsewhere —
  and "Setup Windows VMs" — downloads/imports a specific Windows 11 VM
  image, more plausibly GUI-workflow-coupled). These could legitimately
  warrant **different** scope values, and the proposal's single-row table
  can't express that.
- **Check 4 operates at file granularity.** `grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file"` only checks *that the file
  contains at least one* `hosts:` line, and the awk parser stops at the
  **first** `  tags:` block found anywhere in the file. If a contributor
  (following the checklist's per-file instructions) adds a `tags:` block only
  to the first play ("Install Virtualbox"), the check will report the file as
  correctly scoped — while the second play ("Setup Windows VMs") remains
  completely untagged and would run on **every** profile (`--skip-tags`
  never touches an untagged task). This is a **false pass**: the gate says
  "this file has a valid scope declaration" while a whole play inside it has
  none.

This is confined to one optional/experimental-tier file (not part of the
mandatory core-31, and already flagged for an owner decision on the
scope *value* for unrelated reasons), so I'm filing it as SHOULD-FIX rather
than a blocker — but it should not ship silently as a single classification
row. **Fix**: either (a) add a `tags:` block to **both** `- hosts:` entries
in this file (each play gets its own, independently-decided scope), or (b)
split the file per the graft rule's file-split branch — arguably the cleaner
fix here since the file already has two structurally independent plays, so
splitting doesn't even require *creating* a new play boundary, just moving
one existing block to its own file. Either way, the checklist (§7 step 4)
should call this file out explicitly rather than let it be swept up in the
"apply canonical block to all 41" instruction, which implicitly assumes one
play per file — an assumption that is otherwise true across the whole repo
(I grepped every `.yml` under `playbooks/` for files with more than one
`^[[:space:]]*-[[:space:]]+hosts:` line; this is the **only** one).

### No 4th mixed CORE play found (method disclosed)

I grepped the 18 core plays classified `scope-general` that weren't already
one of the three known mixed plays, for GUI/session/GNOME signal words
(`gsettings|dconf|gnome|DISPLAY=|DBUS_SESSION|flatpak|xdg-open|notify-send| zenity|gtk|\.desktop|wayland|xorg|nautilus|panel|extension`). Two hits
outside the known three:

- `play-mask-intel-lpmd.yml:16` — a **comment** referencing "the GNOME Power
  Mode panel" as rationale/context, no actual task depends on it. Not a
  finding.
- `play-podman.yml` — the existing `dbus_session_check`
  (`systemctl --user status`, `failed_when: false`) and its `when:` guard on
  enabling `podman.socket`. This is a **general**, not GNOME-specific,
  systemd user-session probe (works headless via `loginctl linger`), already
  correctly guarded with `when:` so it no-ops cleanly rather than hard-fails
  if the session doesn't exist — this is the *opposite* of the
  `play-prevent-ssh-suspend.yml` bug (that one had no guard). Not a finding;
  if anything, evidence this play was already written with headless-safety
  in mind.
- `play-claude-yolo.yml` lines 464/491 ("Wayland display forwarding:
  automatic", "Container Extensions") — read the surrounding context: both
  are inside a single `ansible.builtin.debug: msg: [...]` informational
  banner printed at the end of the play. Purely textual output, no
  functional dependency on a display. Not a finding.

I also specifically checked `play-github-cli-multi.yml` (flagged by the
proposal itself as "the largest play in the repo... despite being the
largest") for a hidden browser-based OAuth dependency, since `gh auth login`
can default to opening a browser: the play explicitly deploys a
`GH_BROWSER`-based helper (`gh-print-auth-url`) specifically to print the
device-code URL **instead of** opening a real browser. This is direct
evidence the play was already designed for headless/SSH use — reinforces,
doesn't undermine, the `scope-general` call.

**Caveat on rigor**: this was a targeted grep sweep across 18 files plus two
specific reads, not a full task-by-task re-read of all 18 (that would
duplicate the "exhaustive sweep" PROPOSAL.md already did and the audit
budget doesn't obviously call for redoing it from scratch). I'm confident in
the negative result for the signal words checked; I did not, for example,
verify every single `dnf` package name in every general play for a
GUI-only package hiding in a long list (e.g. play-python.yml's ~25-package
list). Flagging the boundary of what I checked rather than overclaiming
completeness.

---

## 6. Completeness / contradictions in the checklist and elsewhere

### SHOULD-FIX 6.1 — `play-container-watch.yml`'s "interim" edit has no exact diff, unlike the three core mixed plays, even though the checklist requires applying it in this pass

§3.4 correctly argues the graft rule's file-split branch is the *right* fix
for this play (its GNOME-extension block is comparably-sized to its general
block, not a trivial exception) — I verified this by listing its tasks:

```
25 Ensure ccy-helpers containerwatch library directory exists   [general]
33 Deploy containerwatch helper modules                          [general]
47 Ensure user local bin directory exists                        [general]
55 Deploy container-watch CLI wrapper                             [general]
66 Ensure user systemd unit directory exists                      [general]
74 Deploy container-watch user systemd units                      [general]
88 Ensure extension destination directory exists                  [gnome]
96 Deploy container-watch GNOME extension                         [gnome]
110 Probe for a usable systemd --user manager                    [general]
121 Enable and start the container-watch user timer               [general]
134 Container-watch timer enable deferred                         [general]
142 Check if container-watch extension is currently enabled       [gnome]
151 Disable container-watch extension to force reload             [gnome]
161 Wait for container-watch extension to unload                  [gnome]
166 Enable container-watch extension                              [gnome]
180 Container-watch extension enable deferred                     [gnome]
```

9 general / 7 gnome — confirms "comparably-sized" is accurate, not
overstated. But §3.4 then defers the actual split ("out of scope for this
plan's Phase 3") while §7 step 4 still requires the **interim** task-tag
approach to be applied to this file as part of the same pass, with no
before/after diff given (unlike §3.1–§3.3's exact quotes). An implementer
following the checklist has to independently identify and tag all 7 of the
gnome-shaped tasks above from prose alone — miss one, and Check 4 (which is
play-level-only) **cannot catch it**, since the play as a whole still has a
valid `scope-general` tag. This is exactly the "review-discipline gap, not a
tooling gap" the proposal itself names in §8's last bullet, materializing on
day one rather than as hypothetical future risk.

**Fix**: either give this file the same exact-diff treatment as §3.1–§3.3
before implementation, or explicitly pull it out of checklist step 4's
blanket "all 41" instruction and track it as a named follow-up so it isn't
silently done ad hoc.

### NITPICK 6.2 — §5's `docs/playbooks.md` insertion instruction contradicts itself

> "insert...immediately after the existing `## Quick Navigation` section and
> before `## Core Playbooks (Automatically Run)`...(i.e. right after line
> 17's heading in the current file)"

Checked the real file: line 17 **is** the `## Core Playbooks (Automatically Run)` heading (`## Quick Navigation` is line 5). "Before ## Core Playbooks"
and "right after line 17's heading" are opposite instructions — the first
means insert before line 17, the second means insert after it. Trivial fix:
drop the parenthetical or correct it to "immediately before line 17."

### NITPICK 6.3 — docs/playbooks.md's per-play sections for the 3 mixed plays aren't updated

`docs/playbooks.md` has existing `### play-basic-configs.yml` /
`### play-prevent-ssh-suspend.yml` / `### play-vpn.yml` sections (confirmed
present). §5's doc-update instructions only add the new "Desktop vs Headless"
section and the `AnsibleStyle.md` scope-tags subsection — they don't mention
updating these three existing per-play write-ups to note the new split or
(for `play-prevent-ssh-suspend.yml` especially) the bug being fixed. Minor
completeness gap, not required for correctness.

### NITPICK 6.4 — `TOTAL` isn't extended for the new check (cosmetic only, see §2)

Already covered in §2 — filed here only for the record since the brief asked
me to check `qa-all.bash`'s counting mechanism specifically. Has no effect on
pass/fail.

### `play-container-watch.yml` "interim" decision itself — no contradiction, just under-specified (see 6.1)

Re-reading §3.4 and §7 step 4 together: I initially read these as
contradictory (§3.4 says the split is "out of scope," §7 step 4 requires
touching the file anyway) but on close reading they're consistent — §3.4
defers the *file-split*, not *tagging the file at all*, and §7 step 4
correctly says so. Not filing this as a contradiction; the real issue is the
missing diff, already filed as 6.1.

---

## Summary

- **1 BLOCKER**: the `grep -xcE`/`set -e` interaction (§1.1) makes Check 4
  crash the entire `qa-ansible.bash` script — and, via `jq -s`'s
  silent-shrink-on-empty-input behaviour, corrupts `qa-all.bash`'s merged
  JSON for every *other* check too — on exactly the input (a play with zero
  valid scope tags) the check exists to catch. This is not a theoretical
  concern; I reproduced both the crash and the downstream `jq -s` corruption
  empirically. It would fire on literally the first `./scripts/qa-all.bash`
  run the implementation checklist tells the implementer to make (step 6,
  "expect it to fail before step 2/4 land"), and it would fire again on the
  first future contributor who adds an untagged play. The fix is a one-line
  `|| true` on each of two lines, already verified sufficient.
- **4 SHOULD-FIX**: the trailing-`#`-comment parser gap (1.2, conflicts with
  this repo's own "comments explain WHY" convention); the real
  two-plays-in-one-file case in `play-virtualbox-windows.yml` that the
  classification table and the file-granularity QA gate both miss (5.1); the
  under-specified `play-container-watch.yml` interim edit that the checklist
  requires applying without giving an exact diff, for a play-level-only gate
  that can't catch a missed task (6.1).
- **4 NITPICK**: `play-vpn.yml`'s changed-tracking granularity shift, the
  self-contradictory docs insertion-point instruction, the stale per-play
  doc sections, and `TOTAL` not counting the new check.
- Sections verified **solid, no finding**: the `qa-all.bash` zero-edit claim
  (§2, conditional on the blocker fix), the three mixed-play "Before" quotes
  and the gsettings-hard-fail claim (§3), and the entire `--list-tasks`
  container-safety + zero-regression mechanism (§4) — I ran it for real
  against the unmodified repo and it behaves exactly as documented.

**Verdict for the team lead**: this is a strong, well-grounded proposal —
the classification work and the mixed-play analysis are careful and mostly
correct, and I want to be clear the blocker is a small, mechanical fix, not
a design flaw. But it is **not implementable as-is**: Check 4's core loop
will crash `qa-ansible.bash` on its very first real invocation (step 6 of
the implementation checklist would not produce the "expect it to fail"
report it promises — it would produce an unhandled bash abort and a silently
corrupted JSON artifact instead). That one fix (`|| true` on two lines) is
required before Phase 3 starts. The two SHOULD-FIX items worth resolving in
the same round (the container-watch diff and the virtualbox-windows
two-play file) are both small and already well-understood; I'd recommend
folding all three into a `PROPOSAL.md` revision rather than a full new round.
