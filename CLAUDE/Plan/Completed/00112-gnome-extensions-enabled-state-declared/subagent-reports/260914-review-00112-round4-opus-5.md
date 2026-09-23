# QA Review — Plan 00112, commit `491c8313` (round 4, the delta only)

Reviewer: qa-reviewer (Opus 5). Read-only: nothing in the reviewed tree was changed —
every measurement below ran in memory, through process substitution, with no file written.
Scope: only what `491c8313` changes, read against the whole repo. Rounds 1–3 are closed.

**Verdict**: FIX-BEFORE-MERGE.

**The decision asked for: Task 2.3 is not safe to tick as it stands.** Two sentences in
this commit state things measurement contradicts. Both are text-only one-line edits
needing no host. The code change itself is correct and equivalent; fix those two and 2.3
is clear. Finding 3 is a genuine style violation you may reasonably route to Plan 00049
rather than here, since changing file modes needs the deploy Task 2.1 is already waiting
on — it did not enter the 2.3 judgement, and neither did Task 2.2's Plan 00117 blocker.

---

## Should fix

### 1. The deferral's stated reason is false for three of the four shapes it covers

`PLAN.md:113-119` — "a malformed `uuid:` hard-fails the hook, confirmed".

I drove the emitter at `scripts/git-hooks/lib/secret-scan.bash:125-147` (its exact body,
extracted with `awk`, not retyped) and then the whole of `hook_filter_match_lines`,
against doctored vars documents supplied through `/dev/fd`:

| `uuid:` shape                          | allowlist builder                              | filter |
| -------------------------------------- | ---------------------------------------------- | ------ |
| empty string                           | entry silently skipped (`:141-142` `continue`) | rc 0   |
| value containing a space               | emits a live allowlist entry                   | rc 0   |
| value containing a comma               | emits a live allowlist entry                   | rc 0   |
| value containing a newline             | emits two broken lines                         | rc 1 — `grep: Trailing backslash` |
| `gnome_shell_extensions` not a mapping | `SystemExit`                                   | rc 1   |

Those four value shapes are exactly what `validate_uuid` rejects
(`helpers/gnome/enabled_extensions.py:147-158`; cases at
`tests/helpers/gnome/test_enabled_extensions.py:145-165`). **Only the newline one
hard-fails.**

The deferral's *conclusion* survives — none of the others widens the exemption to cover a
real address, so the direction is safe. But "hard-fails, confirmed" is not what the code
does, and it is the sentence a future agent will use to decide this nit is cosmetic.
Round 3 asserted the same thing (its should-fix 1, "Direction is safe (hard fail)"); it
was not measured then either.

**Fix**: *a malformed `uuid:` either yields no exemption or exempts a token that is not an
address — only a newline-bearing one hard-fails (measured) — so what is missing is an
operator message, not a guard.*

### 2. The rename consequence overstates, and it now lives in tracked code

`playbooks/imports/play-gnome-shell-extensions.yml:110-113` and `PLAN.md:90-93` — "a
rename there would then deploy one directory while the applier enabled a different one."

Measured: `_locate` requires `<dir>/metadata.json`
(`helpers/gnome/enabled_extensions.py:196-203`), and
`helpers/gnome/apply_enabled_extensions.py:102-108` turns a `missing` entry into
`GNOME-EXT-FAIL declared-extension-not-deployed`, rc 1. So under the old code:

- renaming the `uuid:` in the vars file **alone** → the copy deploys the old directory and
  the applier **fails the play**; it does not enable a different one;
- renaming the vars entry **and** the repo directory → the copy's `src` no longer resolves
  and `_find_needle` raises `AnsibleActionFail` (`ansible/plugins/action/copy.py:470-476`).

The divergence described needs the newly-named UUID to be on disk already from some other
source. The defect being fixed is real — Task 1.5's "no consumer keeps a copy" and
`docs/playbooks.md:536-539` were both false about the play — but the failure mode was
**loud, not silent**. Say that instead; this is the "narrated a cause I did not confirm"
class `CLAUDE/AgentNotes.md:16` names.

### 3. The rewritten task still violates the file-task rule

`playbooks/imports/play-gnome-shell-extensions.yml:117-120` — `mode: '0755'`, no `owner:`,
no `group:`, against `CLAUDE/AnsibleStyle.md:102-104` ("**Always set** `owner:`, `group:`,
`mode:` on every file task").

The repo's canonical form for this exact operation is three directories away:
`playbooks/imports/optional/common/play-container-watch.yml:101-108` copies a repo
extension directory with `owner`/`group`/`mode: "0644"`/`directory_mode: "0755"`. As
written, `metadata.json`, `README.md` and `stylesheet.css` land executable.

Pre-existing and unchanged by this commit — but the commit **rewrote this task**, and
`CLAUDE/Plan/00049-full-repo-audit/research/extensions.md:170` carries it as an open
finding whose task-name citation this commit has just invalidated.

---

## Nits

1. **The task name overclaims.** `extensions/` holds four extension directories; three are
   deployed by their own opt-in plays (`play-container-watch.yml:19`,
   `play-speech-to-text.yml:16`, `play-remote-desktop-toggle.yml:23`). "Deploy Custom
   Extensions From This Repo" deploys the one in `gnome_shell_extensions.custom`. "Deploy
   Declared Custom Extensions" would not overclaim.
2. **`scripts/test-secret-scan.bash:196-197` names the cases on the opposite axis from the
   fixtures.** `UUID_WITH_TAIL` is the strict-*prefix* case and `UUID_WITH_HEAD` the
   strict-*suffix* one (`:202-203`), and `:200-201` exists to warn about that very
   inversion. Naming the fixtures would read straight.
3. **`PLAN.md:113` records a permanent deferral as an unticked `- [ ] ⬜` inside a `[x] ✅`
   task.** plan-qa passes it today, but that box will never tick and Task 1.8 reads
   incomplete. A prose "Deferred:" line, or its own task, would not.
4. **`JOURNAL/00112-Journal-26-09-14.md:338` drops the `· CATEGORY · REF` fields** the
   file's first nine entries carry. Convention, not policy
   (`CLAUDE/PlanJournalling.md:139-153`), so consistency only.

---

## Checked and clean (measured, not assumed)

- **The loop is equivalent, and fail-fast in every malformed shape.** Evaluated through
  ansible-core 2.19.13's own `Templar`: the current declaration renders `src`/`dest`
  byte-identical to the old literals (`git show d13c10cf:…:114-115`); `custom` key absent →
  `AnsibleUndefinedVariable`; `custom: null` → `task_executor.py:216-220` "The `loop` value
  must resolve to a 'list'"; an entry with no `uuid:` → undefined at `src`. A second entry
  whose directory is absent under `extensions/` fails at `copy.py:470-476`. `mode`,
  `become`/`become_user` and trailing-slash semantics are untouched.
- **The one silent shape is `custom: []`** — zero iterations, reported ok, and *not* caught
  by `declared-extension-not-deployed`, because `declared_extension_uuids` derives from the
  same `.values()` and shrinks with it. Nothing is left claiming the deploy happened (the
  VM checker at `guest-acceptance-desktop.bash:172-196` derives from the same file), and it
  is tracked content visible in a diff. This is strictly better than the old code, which
  deployed regardless of the declaration.
- **"No consumer keeps a copy" is now true, by enumeration.** Grepping every declared UUID
  across the tree leaves only `vars/gnome-shell-extensions.yml`, the two deliberate
  fixtures in `test-secret-scan.bash` (`:162`, `:222` — drift made loud by `:176` and
  `:225`), test data, and a docstring example. `docs/playbooks.md:539` was false before this
  commit and is true after it.
- **The anchor claim, re-measured over its whole population.** All 9 of the 11
  `assert_filter` cases that consult the allowlist (the other 2 pass no root / an absent
  vars file, so no anchor can reach them), under four emitter variants: production 0
  failures; `^` dropped → exactly 1 (`UUID_WITH_HEAD`); `$` dropped → exactly 2
  (`UUID_WITH_TAIL`, `EMBEDDED`); `re.escape` dropped → exactly 1 (the wildcard near-miss).
  The comment is exact in the file's own vocabulary. The unchanged `passed: 24` line above
  it is also consistent: 24 + 3 anchor + 2 escape = the 29 cases today.
- **The interior-dot claim, re-measured.** For every declared UUID I substituted each
  interior dot and tested the result against the email pattern. The declared UUID at `:162`
  has one interior dot, and substituting it leaves a string the pattern does **not** match —
  so the token is never extracted, the exemption is never reached, and the case would report
  nothing and prove nothing. The old comment ("no interior dot to substitute") was indeed
  false; the correction is right. Two other declared UUIDs do carry the property, so
  `DOTTED_UUID` could have been either — not a defect.
- **`secret-scan.bash` is comment-only.** The diff touches lines 104-112 of a comment block;
  `hook_extension_uuid_allowlist`'s body is unchanged. `core.hooksPath = scripts/git-hooks`,
  so the tracked copy is the live gate — a lib edit needs no re-install.
- **Public-repo safety, non-vacuously.** 3 added lines match the email pattern, 0 survive
  the repo's own exemption filter — all three are declared UUIDs quoted inside the
  newly-committed round-3 report. No home paths, IPs, hostnames or usernames in added lines.
- **Fail-fast.** No `failed_when` / `ignore_errors` / `FAIL-FAST-OK` added; no `shell:`
  block; no skip-and-continue logic. The diff removes a hardcoded literal and adds a loop.
- **Version bumps.** Not applicable — none of the six paths is under
  `files/var/local/claude-yolo/`, and there is no Dockerfile, entrypoint or deployed skill in
  the diff. The play keeps its shebang and exec bit.
- **Plan/code sync.** Plan, journal and code landed in one commit; the journal entry is
  appended, not edited; Task 2.3 correctly unticked. Branch reads `## F44...origin/F44` —
  nothing unpushed. The modified/untracked files present belong to another session's
  version-pins work and were not swept in.

---

## Mechanical gates

| Gate                                                              | Result                                                                                                                                      |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `./scripts/qa-all.bash`                                           | **PASS**, exit 0 — 831 files; `secret-scan-tests: passed: 29`; `ansible-syntax: 79 playbooks OK (76 under playbooks/imports/, 3 elsewhere)`; `helper-tests: Ran 1023 tests` |
| `scripts/test-secret-scan.bash`                                   | **triggered** (scanner and suite both changed) — standalone `passed: 29  failed: 0`, exit 0                                                  |
| `ansible-playbook --syntax-check play-gnome-shell-extensions.yml` | **PASS**, exit 0                                                                                                                            |
| `hooks-daemon plan-qa --sweep`                                    | exit 1, **0 block / 2 advise** — Plan 00046 stale path, journal-freshness on 12 older plans. 00112 in neither list; identical to rounds 1–3   |
| `scripts/qa-helper-tests.bash`                                    | **not triggered** — no `helpers/` or `tests/helpers/` file in the diff; `qa-all.bash` ran it anyway, 1023 tests                              |
| `python3 -m helpers.gnome.check_extension_compat`                 | **not triggered** — no `extensions/**/metadata.json` in the diff; `qa-all.bash` ran it anyway, 4 extensions OK                               |
| `extensions` ESLint                                               | **not triggered** — no extension JS in the diff                                                                                             |

No playbook was run — `/workspace` is a CCY container. Every claim above came from the code,
from ansible-core's own templating and action-plugin source, or from the real scanner
functions executed in memory.
