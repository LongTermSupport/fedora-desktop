# QA Review — Plan 00122 Phases 5 and 6 (Task 6.4)

**Verdict**: BLOCK

Reviewed: `0e7c0571` (T5.1), `fa76628c` (T6.1), `1598acaa` (T6.2/T5.5), `3a3e8b93`
(T5.3/T5.4) — the whole Phase 5/6 surface on `F44`. Files: `files/home/.local/bin/lxcfreeze`,
`files/home/.local/lib/freeze/freeze-common.bash`, `scripts/test-lxcfreeze.bash`,
`scripts/test-freezelib.bash`, `scripts/test-podfreeze.bash`,
`playbooks/imports/optional/common/play-lxcfreeze.yml`, `tasks/deploy-freeze-lib.yml`,
`docs/playbooks.md`, `CLAUDE/AgentNotes.md`, `PLAN.md`, `JOURNAL/00122-Journal-26-09-16.md`.

Every mechanical gate passes (bottom of this document). Everything below is what they
cannot see. Each finding was reproduced against the shipped code or by mutating a scratch
copy under `untracked/scratch/`, now deleted.

---

## Blocking

### 1. A failed `lxc-unfreeze` is reported to the user as a success

`files/home/.local/bin/lxcfreeze:759-767`

```bash
freeze_hook_act() {
    local action="$1" name="$2"
    if [ "$action" = "freeze" ]; then
        sudo lxc-freeze -n "$name"
    else
        sudo lxc-unfreeze -n "$name"     # <- status discarded
        renew_dhcp_lease "$name"         # <- runs regardless, and supplies the status
    fi
}
```

Before Task 5.1, `sudo lxc-unfreeze -n "$name"` was the last command in the branch, so its
status *was* the hook's status and the library's `✓`/`✗` line was truthful. Task 5.1
appended a second command and silently transferred the hook's exit status from the thaw to
the *renewal*.

`set -euo pipefail` does not save this. `freeze-common.bash:734` calls the hook as
`if out="$(freeze_hook_act "$action" "$name" 2>&1)"; then`, and bash suspends errexit inside
a command substitution whose value is being tested. Verified directly:

```
$ # source the real tool, stub sudo so lxc-unfreeze FAILS and the nmcli reconnect SUCCEEDS
$ if out="$(freeze_hook_act thaw demo 2>&1)"; then echo "✓ demo"; else echo "✗ demo — $out"; fi
LIBRARY WOULD PRINT:  ✓ demo
```

**Failure scenario.** `lxcfreeze thaw --all` on a host where one container's unfreeze fails
(cgroup error, container stopped between inventory and act, an `lxc-unfreeze` that returns
non-zero for any reason). The renewal's `lxc-attach` succeeds — that container may well
still be reachable at its old address — so `renew_dhcp_lease` returns 0, the library prints
`  ✓ name`, `do_action` counts no failure and `lxcfreeze thaw` **exits 0**. The container is
still FROZEN and the tool has said otherwise. That is the exact class Task 3.6 was opened
to fix (`INV_UNREADABLE` / `warn_unreadable`), reintroduced one phase later on the tool's
other half.

The mirror case is no better: if unfreeze fails *and* the renewal fails, the message the
user sees is `thawed, but the DHCP lease was not renewed — …` for a container that was
never thawed. `PLAN.md:264` states this as a guarantee — *"a failed renewal is a named
failure whose message says the container IS thawed"* — and the code cannot honour it.

HARD RULE, `CLAUDE.md` § Fail Fast: *"NEVER decouple dependent operations — If task B
depends on task A, failure in A must prevent B."*

**Fix**: make the thaw's status gate the renewal, e.g.

```bash
sudo lxc-unfreeze -n "$name" || return $?
renew_dhcp_lease "$name"
```

### 2. The suite pins the defective form and cannot tell it from the fix

`scripts/test-lxcfreeze.bash:471-488`

The Task 5.1 assertions read the *text* of the hook: they locate the source line containing
`lxc-unfreeze -n` and assert the **next line** mentions `renew_dhcp_lease`. Adjacency of two
source lines is not the property that matters; consumption of the first one's exit status
is. Mutation evidence, scratch copy, whole suite:

| Mutant                                                | Result           |
| ----------------------------------------------------- | ---------------- |
| drop `renew_dhcp_lease` from the thaw branch          | 98 / **1 failed** |
| **the code as shipped** (status discarded)            | 99 / 0 passed    |
| **the fix** (`sudo lxc-unfreeze … \|\| return 1`)     | 99 / 0 passed    |

The suite is green for both the broken and the correct version, so it vouches for nothing
here. `scripts/test-lxcfreeze.bash:517-520` gives the reason the act hook is inspected as
text rather than run — *"it shells out to sudo, and this container has no lxc"* — and that
reason is not sound: I ran `freeze_hook_act thaw demo` in this container by defining a
`sudo` shell function, which is all it takes. The act hook is behaviourally testable here,
and a behavioural case is what finding 1 needs.

**Fix**: a case that stubs `sudo`, drives `freeze_hook_act thaw` with a failing
`lxc-unfreeze` and a succeeding reconnect, and asserts the hook returns non-zero.

---

## Should fix

### 3. The `IPV4` cell collapses "no address" into "could not ask"

`files/home/.local/bin/lxcfreeze:507-513`

```bash
addr_rc=0
addr_raw="$(sudo lxc-info -n "$name" -iH 2>&1)" || addr_rc=$?
if [ "$addr_rc" -ne 0 ]; then
    addr=""            # <- indistinguishable from "this container has no address"
```

The column exists precisely to make one blank meaningful: `PLAN.md:273-280` and
`docs/playbooks.md:1021-1023` both say a blank beside `RUNNING` **is** the expired-lease
symptom. A probe that failed now produces the identical cell, so the signal cannot be read.
This is the repo's recurring defect class — a clean result and a blind one printing the same
thing.

The same loop, twelve lines earlier, already does it right and says why:

- `:468-480` — a failed state read goes to `INV_UNREADABLE` and is **disclosed** by
  `warn_unreadable`.
- `:494-500` — *"its status is captured because a config that cannot be read is a different
  fact from a config that declares no network"* → `BRIDGE_NONE` vs `BRIDGE_UNREADABLE`, two
  distinct labels.
- `:507-513` — the address read captures the status and then throws it away.

`scripts/test-lxcfreeze.bash:414-416` even names *"the distinction BRIDGE_NONE /
BRIDGE_UNREADABLE exists to preserve"* while asserting the behaviour that discards it.

**Failure scenario, and it is a whole-column one.** An `lxc-info` build or version where
`-i` is unavailable or errors makes `addr_rc` non-zero for *every* container. The table then
shows every RUNNING container with a blank address — which reads as "every container lost its
lease overnight", the exact alarm Phase 5 was built to raise, produced by a probe that never
worked. Nothing anywhere says the column is blind.

**Fix**: the pattern the file already owns. Pass `addr_rc` into a `lxcf_ipv4_label` beside
`lxcf_bridge_label`, yielding a third value (`(could not read)`), or add the name to
`INV_UNREADABLE`-style disclosure. Note the column is 16 wide and `(could not read)` is 16
characters, so a label needs a width check.

### 4. Task 5.4's library behaviour has no coverage at all in any suite

`files/home/.local/lib/freeze/freeze-common.bash:745-752`, `scripts/test-lxcfreeze.bash:447-456`

The new assertions test the *string constant* `FREEZE_FREEZE_NOTE` — that it is non-empty
and contains `ssh`, `Reconnect` and `lease`. Nothing tests that anything ever prints it.
Mutants against the library, run through all three suites (522 assertions):

| Mutant                                                          | freezelib | lxcfreeze | podfreeze |
| --------------------------------------------------------------- | --------- | --------- | --------- |
| delete the whole `if [ -n "$FREEZE_FREEZE_NOTE" ]` print block   | 236 ✅    | 99 ✅     | 187 ✅    |
| print the note **unconditionally** (blank line for an empty one) | 236 ✅    | —         | 187 ✅    |
| print the "Thaw them with:" block on thaw instead of freeze      | 234 / **2 failed** | — | — |

So the feature can be deleted outright and the repo stays green, and the claim made in
`PLAN.md:286-288`, in the commit message, in the journal and in the library's own comment —
*"an empty note prints nothing at all, not a blank line"* — is asserted in four places and
verified in none. The third row proves this is not a structural limit: a neighbouring
behaviour in the same `if` block **is** killed by two named cases, so `do_action` is already
drivable in `scripts/test-freezelib.bash` (see its `do_action` sections at :610-690).

**Fix**: two cases in `scripts/test-freezelib.bash` — set `FREEZE_FREEZE_NOTE`, run
`do_action freeze`, assert the text appears; leave it empty, run again, assert the output
after the `Thaw them with:` line is unchanged.

*(Checked and clean, in the other direction: `FREEZE_FREEZE_NOTE` being **absent** from the
required-settings loop at `freeze-common.bash:101-102` is correct and is pinned — moving it
into that list makes both `test-freezelib.bash` and `test-podfreeze.bash` abort at source
time, because `podfreeze` does not set it.)*

### 5. `INV_IPV4` is the only inventory array with no declaration

`files/home/.local/bin/lxcfreeze:191-194`, `:680`

```
191  # INV_NAME and INV_STATE are the library's; INV_BRIDGE is this tool's third parallel
192  # array, indexed the same way. …
194  declare -a INV_BRIDGE=()
201  declare -a INV_UNREADABLE=()
```

`INV_NAME`, `INV_STATE` (library `:131-132`), `INV_BRIDGE` and `INV_UNREADABLE` all carry a
file-scope `declare -a … =()`. Task 5.3 added a fourth parallel array and declared it only
inside `load_inventory:463`. Under the tool's `set -u`, reading the table before the
inventory is populated aborts rather than printing an empty cell — verified:

```
/workspace/files/home/.local/bin/lxcfreeze: line 680: INV_IPV4[$i]: unbound variable
```

Not reachable on today's call graph (`freeze_hook_refresh` → `load_inventory` always runs
first), which is exactly why it will not be noticed until a future caller makes it
reachable — the declaration is the defence that the three siblings have and this one does
not. The comment at `:191-193` is also now wrong: `INV_BRIDGE` is no longer the third array,
and the fourth is unmentioned.

**Fix**: `declare -a INV_IPV4=()` beside `:194`, and update the comment to name all four.

### 6. The `die` that the whole `lxcf_parse_ipv4` argument rests on is untested

`files/home/.local/bin/lxcfreeze:348-350`

The commit message, `PLAN.md` and the journal all argue this branch hardest — *"above 1 is
grep failing, and blanking the cell for that would report 'no address' from a probe that
never ran, so it dies."* Replacing the `die` with `printf ""` leaves the suite at **99 / 0**.

The behaviour itself is correct — I confirmed the `die` inside `$( )` does stop the tool
rather than yielding an empty string, because `load_inventory` is called bare at
`freeze-common.bash:772` and `lxcfreeze:839`, so errexit is live on the assignment:

```
lxcfreeze: grep failed (status 2) reading the IPv4 address from lxc-info output
exit=1
```

But nothing pins it, and it is cheap to pin: a `grep() { return 2; }` shell function ahead of
one `if lxcf_parse_ipv4 …` case.

---

## Nits

### 7. `for dev in $devices` — the unquoted-split shape Task 4.1b fixed in `podfreeze`

`files/home/.local/bin/lxcfreeze:749`. Word splitting **and** pathname expansion, which is
verbatim the defect `PLAN.md:214-218` records fixing in `identity_matches` one phase earlier
(*"a label value of `*` expanded against the working directory"*). No realistic `nmcli`
DEVICE name triggers it, so the risk is near zero — the reason to change it is that the
lesson was written down beside the thing it fixed and not generalised to the new code in the
same file. `mapfile -t devs < <(…)` plus `for dev in "${devs[@]}"`. `shellcheck -x -S style`
does not flag this form, so no gate will.

### 8. The second `lxc-attach` does not follow the rule Task 5.5 just wrote

`files/home/.local/bin/lxcfreeze:750`. `CLAUDE/AgentNotes.md` now says *"**Every**
`lxc-attach` in a script or a probe goes through `2>&1 | cat` or a `{ …; } 2>&1 | cat` group,
so both streams are a pipe."* The first `lxc-attach` in the function (`:739-740`) captures
with `2>&1`; this one redirects only stdout and leaves stderr on the caller's fd 2. It is
safe today **only** because its single caller wraps it in `$( … 2>&1)` at
`freeze-common.bash:734`, so fd 2 is a pipe — but that is a property of a caller in another
file, not of the code the rule governs.

### 9. Task 6.3's status marker contradicts the repo's own legend

`PLAN.md:313`: `- [x] 🚫 **Task 6.3**: Cancelled by Task 6.2`. `CLAUDE/PlanWorkflow.md:324-325`
assigns 🚫 to **Blocked** and ❌ to **Cancelled**. The line carries three conflicting signals
(`[x]` = done, 🚫 = blocked, the word "Cancelled"). Task 3.7 at `PLAN.md:168` uses `[ ] 🚫`
for a genuine block, correctly. Should be `- [ ] ❌`.

### 10. A met success criterion is left unticked

`PLAN.md:335` — *"The suspend-to-disk decision is recorded with the spike's evidence"*. It is:
`PLAN.md:293-304` carries the decision and the journal's 10:00/10:02/10:10 entries carry the
evidence. Leaving it unticked while marking the plan Complete under-reports the work.

### 11. A leftover `trim` assertion, and a width check the new column weakened

`scripts/test-lxcfreeze.bash:383` still reads `trim "$(freeze_hook_table_row 3)"`, three lines
below the comment at `:375-377` explaining why that is wrong on a two-column row. It passes
only because row 3's IPV4 cell happens to be blank — the precise "passes for the wrong reason
whenever the one being asserted happens to be last" case. Give `kilo` an address in the
fixture and it breaks confusingly. Use `col … 1`.

`:388-393` now measures the **whole 33-character row** against `${#BRIDGE_UNREADABLE}` (16).
Before Task 5.3 that was loose; with a second 16-wide column padding the total it is
vacuous — narrowing the BRIDGE column to `%-8s` would ragged the table and still pass here.
It should measure the first column, not the row. (The `16` itself is adequately pinned: the
header assertion at `:379` hardcodes `%-16s%-16s`, so a width change fails loudly.)

### 12. `grep -m1` behind `pipefail` misreads SIGPIPE as "grep failed"

`files/home/.local/bin/lxcfreeze:343-344`. `grep -m1` exits as soon as it matches; with
`set -o pipefail` the `printf` feeding it can then die of SIGPIPE (141), which becomes the
pipeline's status and trips the `> 1` guard at `:348`. Reproduced — but only past the 64 KB
pipe buffer:

```
n=1 OK -> [192.0.2.7]      n=5000 OK -> [192.0.2.7]      n=200000 DIED -> grep failed (status 141)
```

Unreachable with real `lxc-info -iH` output, so this is a latent misclassification rather
than a live bug. `grep … <<< "$raw"` removes the pipeline and the trap together.

### 13. Thaw now fails for any container without NetworkManager, and nothing says so

`files/home/.local/bin/lxcfreeze:739-748`. A container using `dhclient`, `systemd-networkd`
or a static address has no `nmcli`; `lxc-attach` returns non-zero and `lxcfreeze thaw` now
exits 1 for a thaw that succeeded. Failing loudly is the house style and the message is
accurate, so this is not a defect — but `docs/playbooks.md:1014-1017` presents the renewal as
an unconditional improvement and should say that thaw's exit status now depends on
NetworkManager being present in every container thawed.

### 14. The inventory now makes two `sudo lxc-info` calls per container

`files/home/.local/bin/lxcfreeze:476` and `:508`. Three privileged round trips per container
per `load_inventory`, and `interactive_loop` re-runs it every pass
(`freeze-common.bash:772`). `lxc-info -n NAME -si` would answer both in one call at the cost
of a slightly larger parse. Worth knowing on a host with many containers; not worth changing
on a small one.

---

## Checked and clean

- **IaC graph placement** — correct. The renewal, the parser and the note's *text* are all in
  `lxcfreeze`; the library gained only a declared, defaulted slot and a guarded print. No `if`
  on an engine name entered the shared half, and the LXC facts appear in `freeze-common.bash`
  only as a doc comment explaining why the slot is empty by default. `podfreeze` correctly
  gets no freeze note — a paused Podman container has no DHCP client and no long-lived ssh
  session of its own — and the empty default costs it nothing.
- **Play and deployment** — Phases 5–6 added no artefact, so
  `playbooks/imports/optional/common/play-lxcfreeze.yml` needed nothing beyond its ready
  message, which it got (`:121-123`). `tasks/deploy-freeze-lib.yml` is still included by both
  plays. Shebang and exec bit intact; `--syntax-check` passes. Nothing under
  `files/var/local/claude-yolo/`, so no `CCY_VERSION` or container-version bump is in scope.
- **Fail-fast greps** — no `|| true`, no `failed_when: false`, no `ignore_errors: true`, no
  skip-and-warn anywhere in the Phase 5/6 diff. The two decisions the brief asked me to judge:
  `lxcf_parse_ipv4` treating `grep` rc 1 as an answer and dying above 1 is **correct** reasoning
  and correctly implemented (verified, see finding 6); `load_inventory` keeping the row on a
  failed address read is **correct** — state gates the verbs — but the cell it leaves is not
  (finding 3).
- **Stderr hygiene** — clean. `lxcf_parse_ipv4`'s stdout carries only the address
  (`printf '%s'`, no newline) and its `die` goes to stderr via `freeze-common.bash:162`. The
  freeze note is `>&2`. `renew_dhcp_lease` echoing to **stdout** is right, not a violation:
  the hook contract at `freeze-common.bash:67` makes the hook's output the captured payload
  the library prints, and `> /dev/null` on the reconnect keeps `nmcli`'s own stdout out of
  it. "see nmcli output above" is accurate — `nmcli`'s stderr is merged into the same capture
  and is written before the message.
- **Public-repo safety** — clean. The Phase 5/6 additions carry no private IP, home path,
  hostname, username, container ID or network name. Fixture addresses are RFC 5737
  (`192.0.2.11/12`, `198.51.100.5`) and RFC 3849 (`2001:db8::1`), with a comment citing
  `CLAUDE/ExampleValues.md`. The journal's live DHCP capture is anonymised to `<container-A>`
  and `<pid>`. `lxcbr0` / `virbr1` are distribution defaults, not this host's names.
- **Journal discipline** — append-only and monotonic. All three Phase 5/6 commits touch the
  journal with zero removed lines and hunks at EOF (`@@ -99,3`, `@@ -143,3`, `@@ -161,3`);
  times run 09:20 → 12:05 without inversion. A `handoff` entry is owed when Task 6.4 closes.
- **Docs** — `docs/playbooks.md` matches the code, with the two caveats at findings 3 and 13.
  `CLAUDE/ContainerEngines.md` needed no change (it owns the engine split, not tool
  behaviour). The `#play-podfreezeyml` anchor `docs/ccy.md` links is untouched.
- **Plan Commit Rule** — satisfied. Every Phase 5/6 commit stages `PLAN.md` and the journal
  alongside the code, and no untracked `CLAUDE/Plan/` directory is left behind.

## The plan cannot be marked Complete yet, independently of the above

`PLAN.md` still carries **Task 3.7** (`🚫`, owner's call — the `.semgrep` `|| true` rule
widening, 18 live sites across 8 files including two git hooks) and **Task 4.6** (`⬜`, HOST —
`podfreeze`'s fzf path, which the 09:45 journal entry explicitly leaves to the owner). Four
Success Criteria are unticked, one of them legitimately so: the >1-hour-lease ssh case is
recorded at `PLAN.md:270-272` as "confirmed the next time it happens", which is honest and
should stay unticked rather than be waved through.

## Mechanical gates

- `./scripts/qa-all.bash`: **PASS** — 924 files; `freezelib: 236`, `lxcfreeze: 99`,
  `podfreeze: 187`.
- `hooks-daemon plan-qa --sweep`: **PASS for 00122** — 2 findings repo-wide (0 block, 2
  advise), both stale-journal advisories on other plans (00099, 00075). Nothing on 00122.
- `ansible-playbook --syntax-check playbooks/imports/optional/common/play-lxcfreeze.yml`:
  **PASS**.
- `shellcheck -x -S style` on `lxcfreeze`, `freeze-common.bash`, `test-lxcfreeze.bash`:
  **clean** apart from the pre-existing, documented SC2119 on the deliberately-bare
  `assert_on_host` (`lxcfreeze:837`).
- Conditional gates, per `CLAUDE/QA.md`: the Phase 5/6 diff touches no `helpers/`,
  `tests/helpers/`, `extensions/` or `metadata.json`, so `qa-helper-tests.bash`,
  `check_extension_compat` and the extensions ESLint run are **not triggered** and were not
  run.

**Working tree**: I changed no tracked file. Scratch copies were made and deleted under
`untracked/scratch/`. Note that `git status` was already dirty when I finished — 11 modified
files under `extensions/`, `helpers/host_health/`, `helpers/gnome/` and `tests/` — none of
them mine and none in this plan's scope; they appeared during the review from concurrent
work.
