# QA Review — branch F44, `e0feee15..d3ec368` (9 commits)

**Verdict**: BLOCK

Reviewed at HEAD `d3ec368`. Scope re-established with `git log`/`git diff` rather than
from the brief; the two commits added after the first pass (`6bab566`, `d3ec368`) are
covered in their own section below.

## Tree state at review time

HEAD is `d3ec368` and clean for everything reviewed. The **working tree is dirty with
in-flight work** that is not part of this review:

```
 M files/var/local/claude-code/cc                          <- return-code contract 0/2/3
 M files/var/local/claude-yolo/lib/token-management.bash
```

That in-flight edit is addressing blocking finding 1 below, and `qa-all.bash` is
currently RED because `test-ccy-token-mode.bash` still asserts `rc=1` where the new
contract returns `rc=3`. Expected mid-fix; noted so nobody reads the red as a regression.
Forward-looking: changing container mode's answer from `1` to `3` is a contract change.
`claude-yolo:1099` and `:1212` both use `select_token … || { … }`, which tolerates any
non-zero, so `ccy` is safe — but the gate encodes `1`, so both must move together.

---

## Blocking

### 1. `if ! select_token` disables `set -e` for the whole function — Ctrl-D at the host `cc` chooser spins forever
`files/var/local/claude-code/cc:139` (as committed at `d3ec368`)

Bash suppresses `errexit` for the entire dynamic extent of a command in an `if`/`!`
condition. The pre-diff call was bare (`select_token "$TOKEN_DIR" "host"`), so `errexit`
was live inside the function. Wrapping it in `if !` turns that off for every command in
`select_token`, including the `read -r -p` at `token-management.bash:1216`, whose failure
on EOF was previously fatal.

Probe against the committed HEAD blobs (`common-pure.bash` + `token-management.bash` +
a one-token fixture pool, stdin `</dev/null`):

```
--- HEAD blob, PRE-diff call shape (bare) ---   lines=13        (clean exit 1)
--- HEAD blob, POST-diff call shape (if !) ---  exit=124 lines=1273575
```

Tail of the post-diff log: `Invalid selection: (empty)` repeating. `read` fails on EOF,
`selection` is empty, the `[ -z "$selection" ]` branch at `token-management.bash:1219`
`continue`s, `read` fails again — an unbounded busy loop at ~250k lines/second. Previously
`cc` exited cleanly.

Violates `CLAUDE/InteractiveScripts.md` Rule 02 (bounded retries — "a piped or runaway
input cannot spin forever") and Rule 03 ("`read` returning EOF … MUST abort cleanly with a
message — never loop on empty input"). Per that file's litmus test at line 22 ("shows a
menu … these rules bind it"), `cc` is in scope.

Aggravating: `ccy` calls `select_token … || { … }` (`claude-yolo:1099`, `:1212`), also a
condition context, so container mode already has this. Reproduced: `exit=124 lines=1346690`.
Pre-existing there, newly inherited by `cc`.

**Fix** — in `select_token`, check `read`'s status
(`if ! read -r -p … selection; then … return 2; fi`) and cap the retry loop. That closes it
for both launchers at once, which is where it belongs. The in-flight working-tree edit
appears to be doing exactly this.

---

## Should fix

### 2. Every non-zero return from `select_token` is reported as one specific cause
`cc:140-151` (at `d3ec368`)

The handler asserts "Every token in the pool is past the expiry date recorded in its
filename." That is not what a non-zero return means. `is_token_valid`
(`common-pure.bash:123`) returns 1 for an **old-format token with no date in the filename**
— the message then names a date that does not exist; and `token-management.bash:1300` has a
second `return 1` after the redraw loop. With finding 1 in play, an internal failure
anywhere in the function also surfaces under this message. Give the expired-only case a
distinct status and treat any other non-zero as an internal error. (The in-flight edit does
this.)

### 3. The post-park assertion's stated rationale is factually wrong, in three places
`cc:230-241`, `docs/ccy-changelog.md:44`, `JOURNAL/00048-Journal-26-09-12.md:109`

All three say a failed park "previously left the credential in place and launched anyway" /
"degrades silently to Desktop auth". `cc:17` is `set -e` and the `mv` is in an `if` **body**,
where errexit applies. Probed:

```
$ mv /nonexistent/src /nonexistent/dst   # inside an if body, set -e
mv: cannot stat '/nonexistent/src': No such file or directory
exit=1                                   # "REACHED the post-mv assert" never printed
```

A failed `mv` already aborted, loudly, with `mv`'s own message. The assertion is harmless
belt-and-braces, but it covers only a `mv` that exits 0 **and** leaves the source — not a
state `mv` produces. Keep the code; correct the three claims (`CLAUDE/AgentNotes.md`,
"Never assume or hallucinate").

### 4. `DECISIONS.md` still records the opposite decision
`CLAUDE/Plan/00048-cc-token-source-parity/DECISIONS.md:137-139`

Decision 7 reads: "`CLAUDE_CONFIG_DIR` isolation was rejected: its scope is undocumented and
would likely orphan host `settings.json`, MCP servers and project history." The code now
honours it, and only a **code comment** (`cc:64-66`) says Decision 7 is settled.
`DECISIONS.md` is untouched by this branch (last change `03ccaaf5`). A reader of the decision
record gets the superseded answer. Add a superseding note there — the journal already
demonstrates the right pattern with the `d3ec368` correction entry.

### 5. The `CLAUDE_CONFIG_DIR` symptom is narrated in the past tense but never happened
`cc:61-66`, `docs/ccy-changelog.md:41-44`

"Hardcoding `$HOME/.claude` here **meant that** … the session **ran** on the Desktop account
while `/status` still **reported** the env token as active." Nothing in this repo sets
`CLAUDE_CONFIG_DIR` — the only references are these comments and Plan 00098 research
(`git grep CLAUDE_CONFIG_DIR`). The journal's original wording at line 107-109 is correctly
conditional; it hardened into observed history on the way into the code and changelog. The
*scope* claim itself is properly sourced — `research/raw-findings.md:240` carries the
documented wording verbatim, marked `[confirmed]`. Only the symptom narrative is unearned.

### 6. Relocating `CRED_FILE` orphans any existing backup
`cc:67-69`

`CRED_BAK` now derives from `CLAUDE_CONFIG_HOME`. A user hard-killed *before* setting
`CLAUDE_CONFIG_DIR` has a parked Desktop credential at
`$HOME/.claude/.credentials.json.cc-desktop-bak`; the self-heal at `cc:76-79` will never look
there, and the credential is lost silently. Narrow, but the same silent-loss shape this
commit set out to close.

### 7. The new gate's discrimination check is tautological, and its comment says otherwise
`scripts/test-ccy-token-mode.bash:159-178`

The comment claims: "If host mode returned 1 unconditionally the fix case above would pass
while the designed Desktop short-circuit was broken". That is false — the check at line 121
(`empty pool -> Desktop`, want `rc=0`) already asserts exactly that, and would fail.

`host_empty_rc` re-runs the same call as check 5 (asserted `0`) and `host_expired_rc`
re-runs check 6 (asserted `1`). If both pass, `0 ≠ 1` and the discrimination check
**cannot** fail. It can never be the sole failing assertion, so it carries no independent
signal. Empirically confirmed — my independent RED reproduction (below) shows it failing in
lockstep with check 6, two failures for one defect:

```
  FAIL  expired tokens only -> refuse   -> rc=0 (want 1) sel=EMPTY (want EMPTY)
  FAIL  host mode answers an empty pool and an expired pool identically (rc=0)
passed: 8   failed: 2
```

Its one genuine (thin) property is that it re-invokes `select_token`, so it would catch
non-determinism or state contamination between calls. Say *that* in the comment, or drop the
check and reclaim the case count — do not leave a control documented as catching something
the assertions above it already catch.

### 8. The gate does not state its coverage boundary — and the uncovered majority is where the live defect sits
`scripts/test-ccy-token-mode.bash:21-25`, `:182`

`select_token` has **10 `return` points** (`token-management.bash` lines 1040, 1047, 1049,
1112, 1116, 1243, 1260, 1276, 1290, 1300). The suite exercises **4** of them: 1047, 1049,
1112, 1116. Uncovered: invalid mode (1040), host `d`/Desktop from the menu (1243), container
renew (1260), container create (1276), numeric selection (1290), and the post-loop
`return 1` (1300).

The header comment presents "every case here is non-interactive … returns before
`select_token` reaches its `read -p` menu" as a *feature*, and never as the resulting
coverage limit. The pass line prints `passed: 10` — a count of assertions, not a statement
of population. Per `CLAUDE/AgentNotes.md` ("a result whose coverage is implied by the length
of a list rather than stated as a number"), this needs a `COVERAGE: 4 of 10 return paths`
line naming what is out of scope.

This is not academic: **blocking finding 1 lives at line 1216, inside the untested menu
path.** The gate is green and the defect is live, and nothing in the output distinguishes
"this code is good" from "this code was not looked at".

---

## Nits

9.  `docs/ccy-changelog.md` / journal line 131: "Container mode is **byte-for-byte** unchanged
    in behaviour." The bytes changed; the behaviour did not. Say behaviour.
10. `cc:81-88` states one cause for the both-files state as certain. `cc` takes no lock, so
    concurrent `cc` sessions could not be ruled out as a second producer — in which case the
    offered `rm -i '$CRED_BAK'` would destroy a live session's parked credential. No
    reproducer established; soften the wording or take a lock.
11. `cc:96` says "Both conditions are load-bearing" above a three-condition test.
12. The journal's RED excerpt (`JOURNAL/00048-Journal-26-09-12.md:207-213`) splices two
    non-adjacent output lines and reads as one discriminating failure. The real run produces
    **two** failures (`passed: 8 failed: 2`) — check 6 and the redundant discrimination
    check. Minor, but it is the evidence block for a gate-quality claim, so it should show
    what actually printed.
13. `WORK="$REPO_ROOT/untracked/ccy-token-mode-fixtures.$$"` is safe (see below), but `$$`
    collides across PID namespaces sharing one bind-mounted repo. `mktemp -d` under
    `untracked/` removes the case entirely.

---

## The new gate commits (`6bab566`, `d3ec368`) — verified independently

**The RED claim is true.** I did not take it on trust: I mirrored `scripts/` +
`files/var/local/claude-yolo/lib/` into a scratch tree, swapped in
`git show e5aee8c^:…/token-management.bash`, and ran the suite from the mirror (no repo
mutation). Result:

```
container mode:  3/3 PASS   (unchanged, as claimed)
host designed:   2/2 PASS   (missing dir, empty pool — both still rc=0)
host fixed case: FAIL  expired tokens only -> refuse -> rc=0 (want 1)
exit code: 1
```

Exactly the fixed case went red while the container cases stayed green. **The gate
discriminates; it cannot pass against the pre-fix library.** The only correction is finding
7 — two assertions failed, not one.

**The library really was restored byte-identical.**
`git diff e5aee8c..HEAD -- files/var/local/claude-yolo/lib/token-management.bash` is empty,
as are the diffs for `cc` and `claude-yolo`. Verified, not assumed.

**No state leakage between cases.** `check()` declares `desc dir mode wantRc wantSel rc
selState` `local`, and resets `SELECTED_TOKEN=""` *without* `local` (line 90) — correct, it
must stay global for `select_token`'s assignment to be visible. The discrimination block
resets it too (lines 166, 169). `select_token`'s own variables are all `local`. The
`GH_TOKEN`/`IMAGE_NAME` stubs (line 47) match `cc:32`, and matter because the suite runs
under `set -u`; the paths that read them are unreachable here anyway.

**Fixture cleanup is correct on failure paths.** `trap cleanup EXIT INT TERM` (line 72) is
set after `WORK` is assigned and before `mkdir -p`. The two early `exit 1`s (lines 41, 62)
precede the trap but also precede `mkdir`, so there is nothing to leak. Verified empirically:
after the failing RED run, `ls -d …/ccy-token-mode-fixtures.*` found nothing — cleaned up.

**The fixture dir is safe and cannot escape the repo.** `REPO_ROOT` is `$SCRIPT_DIR/..`,
script-relative not cwd-relative, so it is immune to the Plan 00068 `git rev-parse` failure.
`untracked/` is tracked via `untracked/.gitignore` (`*` + `!.gitignore`), so it exists in a
fresh clone and its contents are ignored; `git check-ignore -v
untracked/ccy-token-mode-fixtures.99999/x` confirms the fixture path is ignored. `rm -rf
"$WORK"` cannot reach `/` or `$HOME`: `WORK` always carries at least
`/untracked/ccy-token-mode-fixtures.N`, and `set -u` catches the unset case.

**The `qa-all.bash` wiring is correct.** The RED suite exits `1` (measured), and
`scripts/qa-all.bash:217-221` turns that into `exit 1` for the whole run — confirmed live:
the currently-dirty tree makes `qa-all.bash` exit 1 with `✗ QA FAILED: ccy token-mode unit
tests`. The summary extraction degrades correctly, tested both ways:

```
no matching line -> [passed]        # grep exits 1, || fallback fires
"passed: 10 …"   -> [passed: 10]
```

The `x=$(…) || x=fallback` shape is a `||` list, so `set -e` does not pre-empt the fallback.
Identical to the rootless-guard block above it. The gate prints a pass line carrying a
**number** (`✓ ccy-token-mode: passed: 10`), so it is distinguishable from a gate that is not
running — the right shape.

**The `CLAUDE/QA.md` counts are correct — I counted them myself.** `qa-all.bash` invokes
exactly **nine** non-jq-merged gates (lines 108, 120, 143, 159, 176, 194, 217, 233, 243),
matching the nine table rows at `CLAUDE/QA.md:40-48`. The `test-*` suites in that table are
`test-secret-scan`, `test-planlib`, `test-ccy-rootless-guard`, `test-ccy-token-mode`,
`test-qa-ansible-failfast` = **five**. Both edits are right.

**Journal append-only holds across all three commits**, verified mechanically:
`3ae3a67` = 112 insertions / 0 deletions; `e5aee8c` = 64 / 0; `6bab566` = 48 / 0;
`d3ec368` = 31 / 0. Nothing earlier was edited. `d3ec368` is the correct pattern for a
correction: a new dated entry that supersedes, rather than a rewrite. Its substance checks
out — host mode does already advertise and handle `d` (`token-management.bash:1201`,
`:1240-1243`), so shape B is cheaper than the 15:52 entry implied, and the sharpened cost
statement for shape A is accurate.

**Gate placement** is right: permanent gate in `scripts/`, not a plan-local script, following
`test-ccy-rootless-guard.bash` exactly. Committed mode `100755`, matching its sibling.
`shellcheck -x` and `bash -n` clean on both the new suite and the edited `qa-all.bash`
(re-run independently). Live green run: `passed: 10   failed: 0`.

---

## Checked and clean (earlier commits)

- **Container-mode behaviour** — traced every path through `select_token` for
  `mode=container`. The new `&& [ ${#expired_tokens[@]} -eq 0 ]` is appended to a test
  already gated on `[ "$mode" = "host" ]`, which container mode never satisfied; it falls to
  the pre-existing `return 1` at `:1116`. `ccy` is genuinely unaffected, and the new gate now
  asserts this rather than leaving it to a code reading.
- **`expired_tokens` scope/population** — `local expired_tokens=()` at `:1054`, filled at
  `:1057-1065`, read at `:1109`. Always in scope, always defined; same pattern `valid_tokens`
  already used, so no new `set -u` exposure.
- **`print_error` scope** — defined `common-pure.bash:25`, sourced at `cc:48`, before every
  new call site. Writes to stderr.
- **Refusal ordering** — the both-files check (`cc:80-107`) runs before `select_token`,
  before `claude update`, before `write_status_token`, before any park. The only earlier
  mutation is the self-heal `mv -f`, whose guard (`! -f CRED_FILE`) is mutually exclusive
  with the refusal's (`-f CRED_FILE`). Correct.
- **EXIT trap vs `mv` without `-f`** — the trap is set only after a park the assert proved
  took, and its body is guarded by `[ -f "$CRED_BAK" ]`. Dropping `-f` is safe given the
  up-front refusal. If the assert ever fires, `cc` exits 1 with the trap unset, leaving the
  both-files state the next run refuses — self-describing, not silent.
- **Stderr hygiene** — every line added to `cc` is `>&2`, explicitly or inside a `{ … } >&2`
  group. No new stdout output.
- **Quoting / `local`** — no unquoted expansion, no missing `local`; `shellcheck -x` clean on
  the HEAD blobs of `cc` and `token-management.bash`.
- **Version bump** — `CCY_VERSION` 3.49.2 → 3.50.0 with a rewritten comment, staged alongside
  the `lib/` edit. No image-baked file changed (no `Dockerfile`, no `entrypoint.sh`), so
  `REQUIRED_CONTAINER_VERSION` 2.36 and the Dockerfile LABEL correctly stay put.
- **Playbook fix** — `play-claude-yolo.yml:356` uses `/home/{{ user_login }}/…`, matching
  `play-claude-code.yml:28` and the other 7 paths in the same play; the play is
  `become: false`. Generalisation checked: `git grep` finds **no remaining**
  `ansible_facts['env']['HOME']` in `playbooks/` — only explanatory comments. The Plan 00088
  lesson was applied repo-wide, not just beside the fix.
- **Plan honesty** — Task 4.3 is `🔄`; 2.4, 4.6, 4.7 remain `⬜`; header reads "In Progress
  (Phases 1-3 done; Phase 4 awaits a host deploy)". Nothing claimed as verified that was not.
  Plan and code landed together (Plan Commit Rule met).
- **Public-repo safety** — scanned all added lines across the full range: no emails, no
  RFC 1918 addresses, no literal `/home/<name>`. `property-ai` is gone from every tracked
  file. **But** it survives in pushed history, and the commit that removes it (`3ccc2a9`)
  republishes it as a `-` line — the stated non-action on `6f50439a` applies here too. The
  `~/mnt/<drive-folder>` replacement conforms to `CLAUDE/ExampleValues.md`. The new test's
  fixtures hold `placeholder-not-a-real-token`, never a credential.
- **Daemon upgrade commits** — `.claude/hooks-daemon.env` exists, mode 600, gitignored
  (`.claude/.gitignore:10`); `.claude/settings.local.json` genuinely does not exist, so
  `e8ec13f`'s seed swap is correct. No dangling references to the deleted
  `notification_logger` or `.claude/skills/optimise/`. `CLAUDE.md` indexes both new topic
  files.
- **No doc drift** — `docs/` describes `cc` only at `architecture.md:122-126` (import
  ordering), untouched by the change.

---

## Out of diff, but live — reported rather than dismissed

`scripts/qa-all.bash:107-112` runs `qa-nokill-containerwatch.bash` and prints **nothing on
success**. The very next block (line 125) carries the comment *"A gate whose only visible
output is a failure is indistinguishable from a gate that is not running"* and acts on it.
The rule is written down six lines below the one gate that still breaks it — the same
"lesson written down beside the thing it fixed, never generalised" shape as the
`qa-bash`/`qa-python` precedent in `CLAUDE/AgentNotes.md`. Confirmed against a real run: no
`nokill` line appears anywhere in `qa-all.bash` output. One `printf '✓ nokill-containerwatch:
…'` closes it.

---

## Mechanical gates

- **`qa-all.bash`** — green at `✓ QA passed: 697 files checked` earlier in the session
  (exit 0). The 696→697 delta from the team-lead's runs was the then-untracked
  `scripts/test-ccy-token-mode.bash`, not an overclaim; re-running `qa-bash`/`qa-patterns`
  individually gave 206/206, matching. The final run on the current tree exits **1**, caused
  solely by the uncommitted in-flight `select_token` return-code change (the gate asserting
  `rc=1` against a library now returning `rc=3`) — not by anything committed.
- **`plan-qa --sweep`** — 3 findings: 1 block on **Plan 00046** (`Not Started` header over 7
  ticked boxes), pre-existing and unrelated to this branch; 2 advisories (staleness, journal
  freshness). **Plan 00048 is not flagged.**
- **syntax-check** — not run standalone, per the caller's "do not run ansible" constraint.
  Covered by `qa-all`'s gate: `✓ ansible-syntax: 78 playbooks OK`.
- **`bash -n` / `shellcheck -x`** — re-verified independently against `git show HEAD:` blobs
  of `cc` and `token-management.bash`, and directly on `scripts/test-ccy-token-mode.bash` and
  `scripts/qa-all.bash`. All clean. The team-lead's claim holds.
- **Conditional gates** — no `helpers/`, `tests/helpers/`, extension `metadata.json` or
  extension JS in the diff, so `qa-helper-tests.bash`, `check_extension_compat` and ESLint
  were not separately required; `qa-all` ran them anyway (277 helper tests, 4 extensions).
