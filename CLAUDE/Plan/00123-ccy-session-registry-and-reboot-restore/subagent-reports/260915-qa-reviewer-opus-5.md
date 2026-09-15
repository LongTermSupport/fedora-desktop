# QA Review — branch `worktree-ccy-reboot-restore` vs `F44` (Plan 00123)

Reviewed: 3 commits, 22 files, +3933/-60. Worktree:
`/workspace/untracked/worktrees/worktree-ccy-reboot-restore`.

**Verdict**: FIX-BEFORE-MERGE — 2 blocking, 5 fix-before-merge, 8 should-fix, 6 nits.

Headlines:

- **BLOCK** — `mapfile … < <(ccy_registry_list …)` throws the listing failure away in four
  places; the boot service then exits **0** with "nothing to restore" when it could not read
  the registry.
- **BLOCK** — `stale` is measured from the session's **start time**, so a long-running session
  is retired instead of restored at the reboot the feature exists to survive.
- **FIX** — `CCY_UNATTENDED=1` is set on the tmux *client's* scope, not on the session;
  delivery is asserted nowhere (the test stubs `tmux`).
- **FIX** — the transient scope unit is named from the project **directory**, so two sessions
  in one project collide; the second is consumed then lost.
- **FIX** — `ccy-sessions reboot --in N` and `notify` exit **0** doing nothing when no sessions
  are running; they do not always refuse.
- **FIX** — turning restore off removes the unit file without disabling it first, leaving a
  dangling `.wants` symlink.
- **FIX** — `ccy_registry_retire` converts a failed read into a well-formed record and returns
  success, inside the consume-before-start step D2 depends on.

---

## Blocking

### 1. A registry listing failure is laundered into "nothing to restore"

`files/home/.local/bin/ccy-sessions-restore:234`, and
`files/home/.local/bin/ccy-sessions:132,141,191`

`ccy_registry_list` is carefully written to fail when a directory exists but cannot be read
(`lib/session-registry.bash:446-459`, comment: *"that one is 'could not tell', and it must not
look like 'nothing to do'"*). Every caller then destroys that:

```bash
mapfile -t -d '' records < <(ccy_registry_list "$SESSIONS_DIR")
if [[ ${#records[@]} -eq 0 ]]; then
    echo "No sessions were recorded, so there is nothing to restore."
    exit 0
```

Verified in this container (bash 5.2.15): `mapfile … < <(failing)` returns 0 with an empty
array and `set -euo pipefail` does not fire. The boot service prints the *empty-registry*
sentence and exits 0 having restored nothing, with every recorded session lost for that boot.

`ccy-sessions:129-133` is worse because the comment states the guarantee the code does not
provide: *"An absent directory is zero; a directory that cannot be read is a failure, not a
zero."* It is a zero.

This is the same defect the commit messages boast of having found and fixed in
`audit_sessions` — fixed at one site, not generalised
(`CLAUDE/AgentNotes.md` → "Generalise a fix past the file you were reading").

**Fix**: `if ! raw=$(ccy_registry_list "$dir"); then …fail…; fi` and `mapfile` from the
captured string, so the status is read where it is produced.

### 2. `stale` retires exactly the sessions the feature exists for

`files/home/.local/bin/ccy-sessions-restore:279`

```bash
if [[ -n "$(find "$record" -maxdepth 0 -mtime "+${CCY_RESTORE_MAX_AGE_DAYS}")" ]]; then
    retire "$record" "stale" "older than ${CCY_RESTORE_MAX_AGE_DAYS} days"
```

The record's mtime is set once, at `claude-yolo:3268` — `ccy_registry_write` is called from
exactly one place (grepped) and nothing refreshes it. So mtime is *session age*, not *record
survival age*. A session that has been running for eight days on a machine with eight days of
uptime is retired as `stale` at the reboot it was meant to survive.

`PLAN.md:14` states the use case as *"the machine where some sessions are meant to run
permanently"*. `DESIGN-failure-modes.md` D8 justifies the guard as *"enabling restore on a
machine with an old surviving record would … resurrect a session from an arbitrary past
boot"* — a different question from the one mtime answers.

The test cannot see this: `scripts/test-ccy-session-restore.bash:255` sets the case up with
`touch -d '30 days ago'`, which is the author's mental model (old record = old boot) rather
than the load-bearing case (old record = long-lived session). A partial result wearing a
passing verdict.

**Fix**: compare against the current boot's start time, or store a `written_at` epoch in the
record and retire only records written before the *previous* boot began.

---

## Fix before merge

### 3. `CCY_UNATTENDED=1` is handed to the tmux client, not the session — and nothing tests that it arrives

`files/home/.local/bin/ccy-sessions-restore:203-208`

```bash
systemd-run --user --scope --quiet --collect \
    --unit "ccy-tmux-restore-$(ccy_registry_slug "$dir")" \
    --setenv=CCY_UNATTENDED=1 \
    ... -- tmux -L "$CCY_TMUX_SOCKET" new-session -d -s "$name" -c "$dir" ...
```

`--setenv` covers the process systemd-run starts — the tmux **client**. A tmux pane inherits
the **server's** environment plus tmux's `update-environment` copy-list, which does not
include `CCY_UNATTENDED`. The first session of a run creates the server and so gets it; a
session created on a server that already exists does not.

That case is reachable: `--help` and `docs/ccy.md` both present `ccy-sessions-restore` as a
hand-runnable command, and running it on a live machine with a survivor record from a previous
boot starts that session on the live server — where `Use same configuration? [Y/n]` parks for
ever, which is precisely D6's "largest risk in the feature".

The suite cannot catch it: `scripts/test-ccy-session-restore.bash:67-70` stubs `tmux` and
`systemd-run` as scripts that just exit 0, so the constructed command line is never examined.
D6 is the plan's headline safety property and its **delivery** is verified by reading only.

**Fix** (free, belt-and-braces): `tmux … new-session -d -e CCY_UNATTENDED=1 …`, or
`-- env CCY_UNATTENDED=1 bash -c …`. Add a test asserting the argv the stub received. Confirm
the tmux semantics on the host before relying on the current form — tmux is not installed in
the CCY container, so this half is analysis from tmux's documented client/server model, not a
local measurement.

### 4. The transient scope unit is keyed on the directory, so two sessions in one project collide

`files/home/.local/bin/ccy-sessions-restore:204`

`ccy_registry_slug` is a pure function of the project directory. `ccy_tmux_insulate` names its
scope `--unit "ccy-tmux-$$"` (`lib/tmux-session.bash:322`) — unique per invocation. The restore
uses the directory slug, which is not.

`FACTS-ccy-mechanics.md` F1 establishes that several sessions per project are legitimate, the
restore's own comment at `ccy-sessions-restore:317-319` anticipates *"an earlier record for
the same project in this very run"*, and the first commit message says outright *"a record
cannot be keyed on the directory"* — then the unit name is. The second session's
`systemd-run --unit` hits a name already held by the first (still-live) scope, the start
fails, and the record has **already been consumed** at line 193, so the session is gone.
Reported as ERROR, not silent, but lost.

Not covered by the tests (the stub `systemd-run` ignores `--unit`).

**Fix**: name it from `$name`, which `ccy_tmux_next_name` has already guaranteed unique,
sanitised for a unit name.

### 5. `reboot --in N` and `notify` do not always refuse

`files/home/.local/bin/ccy-sessions:305-308` (`audit_sessions`), `:317` (`cmd_reboot`),
`:363` (`cmd_notify`)

With no running sessions, `audit_sessions` prints `(no sessions are running)` and returns 0;
`cmd_reboot` then loops over an empty listing, calls `ccy_reboot_raise_signal` zero times, and
the dispatcher `exit 0`s. So `ccy-sessions reboot --in 5` on an idle machine **succeeds**,
warns nobody, and reboots nothing — the one shape the design says is unacceptable (*"A command
that rebooted without warning anyone, while looking like it had, would be worse than one that
refuses"*). Same hole in `cmd_notify`. The dispatch brief, `--help` and `docs/ccy.md` all
state these always fail.

**Fix**: raise the refusal before the loop, unconditionally, on the non-dry-run path.

Related, same area: the commands never call `reboot`/`shutdown` at all, so the `--help` text
*"warn every session, wait N minutes, then reboot"* and the `docs/ccy.md` table describe a
contract that does not exist even in outline. Fine while blocked — but say so where the
promise is made.

### 6. Turning restore off removes the unit without disabling it

`playbooks/imports/play-claude-yolo.yml:526`

The off path is a bare `file: state: absent`. `systemctl --user disable` is never run, so
`~/.config/systemd/user/default.target.wants/ccy-sessions-restore.service` survives as a
dangling link, and no `daemon_reload` follows.

`vars/container-defaults.yml:22-24` claims the opposite — *"the systemd --user unit is
REMOVED, not merely left disabled, so that `systemctl --user is-enabled …` is the one source
of truth"* — but a dangling wants-link is exactly what stops `is-enabled` giving a clean
answer (and see finding 10: `ccy-sessions` maps that state to `installed-not-enabled`).

**Fix**: run `systemd: name=ccy-sessions-restore.service, scope=user, enabled=false,
daemon_reload=true` (same `XDG_RUNTIME_DIR` environment and getent-resolved uid as the enable
task) **before** removing the file, gated on the file existing.

### 7. `ccy_registry_retire` turns a failed read into a well-formed record and returns success

`files/var/local/claude-yolo/lib/session-registry.bash:481`

```bash
if ! err=$( { grep -v '^end=1$' "$file"; printf 'retired_reason=%s\nend=1\n' "$reason"; } >"$tmp" 2>&1); then
```

Two independent problems, both measured here:

- **Redirection order.** `>"$tmp"` sets fd1 to the file, then `2>&1` points fd2 at the file
  too. So `err` is **always empty** and the grep's error text is written **into the staged
  record**.
- **Status masking.** The brace group's exit status is `printf`'s, so a `grep` that could not
  read the source returns 0. The function then `rm -f`s the original (line 491) and reports
  success.

Reproduction: group `rc=0`, `err=[]`, staged file first line
`grep: …/nonexistent: No such file or directory`, followed by `retired_reason=x` and `end=1`.

In `start_session:193` this is the consume-before-start step the whole no-loop guarantee rests
on: a failure there reports "consumed", starts the session, and has replaced the evidence
record with an error message.

The same redirection-order bug sits at `lib/session-registry.bash:325` (the record writer):
`print_error "could not write $tmp: $err"` can never name a reason, and printf's stderr lands
inside the record.

**Fix both**: redirect stderr to its own capture (`2>"$errfile"` or a separate substitution)
and check each command's status rather than the group's.

---

## Should fix

### 8. There is already an unguarded human prompt, so D6's "one seam covers every prompt site" is not true today

`files/var/local/claude-yolo/lib/token-management.bash:791-792`

```bash
echo "Press Enter to continue..."
read -r
```

No `-p`, so `_ccy_read_guard` passes it straight through and an unattended launch hangs on it
for ever. Reachable only via `ccy --create-token <name>` on a fresh name, which the restore
never passes — so latent, not live. But `docs/ccy-changelog.md` 3.58.0,
`DESIGN-failure-modes.md` D6 and `PLAN.md` Task 2.4 all state the seam covers *every* prompt,
and the flag-classification guard's own lesson is that an enumeration you do not derive goes
stale.

**Fix**: rewrite as `read -rp "Press Enter to continue... "`, and/or add a QA grep that fails
on a `read` with no `-p` shortly after an `echo` that ends in a prompt.

### 9. `ccy_tmux_current_session` conflates "not a CCY session" with "tmux failed"

`files/var/local/claude-yolo/lib/tmux-session.bash:342-355`, consumed at `claude-yolo:3245`
and `:3294`

The docstring enumerates two ordinary causes of a `return 1` and omits the third:
`tmux display-message -p '#S'` failing. The launcher's `else` branch then prints *"Not inside a
CCY tmux session, so this session is NOT registered for restore after a reboot"* — a statement
that may be false — and the session goes unregistered, silently, on the one path the whole
feature depends on.

Also a regression in `ccy_tmux_banner`: it was `name=$(tmux display-message -p '#S') ||
return 1`, propagating the failure; it is now `|| return 0`.

### 10. `restore_installation_state` collapses "could not ask" into "not enabled"

`files/home/.local/bin/ccy-sessions:109`

```bash
if ! probe="$(systemctl --user is-enabled "$CCY_RESTORE_UNIT" 2>&1)" || [[ "$probe" != "enabled" ]]; then
    printf 'installed-not-enabled\n'
```

An unreachable user bus, a `bad` (dangling symlink — finding 6), a `masked`: all become
`installed-not-enabled`, whose advice is "re-run the play". The linger probe four lines below
gets its own `enabled-linger-unknown`, so the function is inconsistent with itself on the exact
axis D8 exists to keep apart. Give the failure its own answer.

### 11. `enabled-linger-unknown` is missing from the user-facing table

`docs/ccy.md`, "Surviving a Reboot" → the restore-status table has four rows; the code produces
five states and `PLAN.md:42-45` lists five. The missing one is the "could not tell" row — the
one the plan argues hardest for.

### 12. A `--no-supervise` session is restored with `--supervise`

`files/var/local/claude-yolo/lib/session-registry.bash:565`, classification at `:78-83`

`--no-supervise` is classified one-shot and the supervise mode is not recorded at all, so
`ccy_registry_restore_flags` unconditionally appends `--supervise`. Per
`FACTS-ccy-mechanics.md` F3 that gives the restored session an **armed** supervisor:
auto-compaction and goal injection an operator explicitly opted out of. Nothing in `PLAN.md`,
`DESIGN-failure-modes.md` or the journal discusses it (grepped all three).

**Fix**: record the mode and honour it, or write down why overriding it is right.

### 13. `shift` past the end aborts with no message

`files/home/.local/bin/ccy-sessions:322-332`, and the `--minutes` arm of `cmd_notify`

`ccy-sessions reboot --in` (flag last) shifts to consume the value, then shifts again at the
loop foot with `$#` already 0; under `set -e` that exits 1 silently. Measured: `set -- ; shift`
→ rc=1. The friendly "`--in` wants a whole number of minutes" message twelve lines below is
never reached. `CLAUDE/InteractiveScripts.md` asks for strict validation with a clear error.

### 14. The flag-classification guard's population is narrower than its wording

`scripts/test-ccy-session-registry.bash:399-402`

```bash
grep -oE 'arg" ==? "--[a-z0-9-]+' "$LAUNCHER" | grep -oE '\-\-[a-z0-9-]+' | sort -u
```

This sees only the `"$arg"` loop. `--help`/`-h` and `--version`/`-v` are parsed at
`claude-yolo:245` and `:252` via `"$1"` and are invisible to it, as would be any future
`case`-based or `--flag=value` parse site. The comment claims it derives *"every `ccy` flag
from the launcher's own parser"*. Measured: the derived set is 25 flags; the two classification
arrays total 25.

**Fix**: print coverage as a number with its buckets — e.g.
`COVERAGE: 25 of 25 flags parsed via "$arg"; 4 parsed via "$1", excluded` — so a change of
composition is visible in ordinary passing output
(`AgentNotes.md` → "A coverage LOSS can hide inside a rising count").

### 15. `ccy_registry_restore_flags` drops SSH keys if the base64 fails to decode

`files/var/local/claude-yolo/lib/session-registry.bash:530-545`

`mapfile -t -d '' key_list < <(ccy_registry_decode_argv "$keys")` discards the decode status,
so a corrupt `ssh_keys_b64` yields an empty key list and a restore with **no SSH keys and no
complaint** — while the `${#flags[@]} -eq 0` guard at `ccy-sessions-restore:177` still passes,
because `--supervise --continue` are appended unconditionally. Same class as finding 1, one
level down.

---

## Nits

16. `files/home/.local/bin/ccy-sessions-restore` is committed mode **100644**; its sibling
    `ccy-sessions` is 100755. The play deploys it 0755 so nothing breaks, and `qa-bash` found
    it (its partial-discovery guard is intact), but the convention differs one line away in
    `git ls-tree`.
17. `ccy-sessions-restore:229` — `"${DRY_RUN:+}"` expands to the empty string unconditionally.
    Delete it or write the intent.
18. `ccy-sessions` sources `${CCY_LIB}/session-registry.bash` with no existence guard, while
    the guard immediately above names only `tmux-session.bash`; an absent library gives a bare
    "No such file or directory" instead of the "Run playbooks/imports/play-claude-yolo.yml"
    message written for exactly this.
19. `DESIGN-failure-modes.md:18` draws the state tree as `sessions/ccy-<slug>.record`. Records
    are named after the tmux session (`claude-yolo:3244`); `ccy_registry_slug` is used only for
    the systemd unit name and in tests.
20. `cmd_restore_status` prints `project_dir` raw in "Recorded sessions" while
    `list_with_reasons` tilde-abbreviates `$HOME`. Make them agree; the tilde form is the
    better default for output an operator may paste.
21. A failed restore leaves a held-open tmux window (`ccy_tmux_hold_on_failure`) that appears
    in `ccy-sessions` looking exactly like a restored session — the shape D6 argues against,
    for a different reason. Deliberate and explained in the journal; say so in `docs/ccy.md`
    so an operator knows what a held window means.

---

## Checked and clean

- **The `read` shadow, both directions.** All 21 `read` sites in `claude-yolo` and every `read`
  in the seven `lib/*.bash` files classified by hand against `^-[a-zA-Z]*p$`: no non-prompt
  `read` is caught (`-ra`, `IFS= read -r`, `read -r name attached dir` all miss the regex), and
  every `-p`/`-rp`/`-rsp`/`-r -p` prompt is. `${!idx}` prompt-text extraction is correct for
  both the bundled and the separated spellings. The one misclassification in the other
  direction is finding 8.
- **Prefix assignments survive the function wrapper.** Measured on bash 5.2.15: `IFS= read -r
  line` and `IFS=',' read -ra arr` through a `read()` function behave identically to the
  builtin (no whitespace stripping, correct field splitting) and `IFS` does not leak after the
  call. This was the highest-risk property of shadowing `read` and it holds.
- **IaC placement.** No new play; the behaviour lives in `play-claude-yolo.yml`, which already
  owns `ccy-sessions` and the launcher. `scope: general`, `become: false`, `root_dir` from the
  config lookup. Ordering is right: `play-systemd-user-tweaks.yml` (linger + user manager) is
  imported at `playbook-main.yml:15`, `play-claude-yolo.yml` at `:39`. The uid is resolved via
  `getent` + `assert` rather than `| default(1000)` — the fix AgentNotes records.
- **Fail-fast annotations.** No new `failed_when: false` / `ignore_errors:` in the diff;
  `qa-ansible` reports fail-fast patterns OK. The gather-semantics exit code in
  `ccy-sessions-restore` is a defensible non-abort with a stated reason and a non-zero exit.
- **Version bump.** 3.57.0 → 3.58.0 → 3.58.1 with an updated one-line comment; `CCY_LIBS`
  extended with `session-registry`; `CCY_HASH` already derives its set from `lib/*.bash`
  (`claude-yolo:154-166`) so the new library is covered without a list edit.
  `REQUIRED_CONTAINER_VERSION` correctly left at 2.37 — no Dockerfile or entrypoint change in
  the diff. Changelog entries match the code, including the 3.52.0 back-reference (verified
  against `docs/ccy-changelog.md:237`).
- **Public-repo safety.** Scanned the whole diff, the 187-line journal and all three commit
  messages for home paths, usernames, hostnames, emails and IPs: only `example.com` and
  `github.com`. The `.claude/.gitignore` trailing-slash fix is real and correctly reasoned.
- **Plan Commit Rule.** Plan, `CLAUDE/Plan/README.md` row, design docs and journal all land
  with the code; `git status` in the worktree is clean.
- **QA wiring.** Both new suites are in `qa-all.bash` with a visible pass line (93 and 35
  cases), so a skipped gate is distinguishable from a silent one.

---

## Mechanical gates

- **`qa-all.bash`**: every stage green **except** `qa-ansible-syntax` — 82 failures, all the
  gitignored-vault-password-file-not-found error, i.e. the documented worktree wall in
  `WORKTREE-QA-GAP.md`, not this branch's doing. Confirmed by reading the whole run that it is
  the only failing stage.
- **`plan-qa --sweep`**: 0 block, 2 advise — neither relates to Plan 00123 (a stale path in
  Plan 00046, journal-freshness on twelve older plans).
- **`ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`**: rc=0, run with
  a throwaway file in `--vault-password-file` flag position, per the form
  `WORKTREE-QA-GAP.md` documents.
- **New suites run directly**: `test-ccy-session-registry.bash` 93/0,
  `test-ccy-session-restore.bash` 35/0.
- **Conditional gates**: no `helpers/` or `tests/helpers/` change and no `extensions/` change,
  so `qa-helper-tests.bash`, `check_extension_compat` and ESLint were not separately required
  — all three ran inside `qa-all.bash` anyway and passed (1341 helper tests, 5 extensions,
  8 JS files).

## Reviewer notes

- This role has no `Write`/`Edit`; the report was written via Bash only because the
  `subagent_report_size_blocker` stop hook required a file. No other file was modified.
- Running the QA gate left a log at `/workspace/untracked/scratch/qa-all-00123.log`. Delete it
  if unwanted.
