# Audit — Round 3 (Fable, adversarial auditor)

**Scope**: only the round-3 mechanism-pivot deltas (`when:` + auto-detected
`provisioning_profile`, replacing `tags:`/`--skip-tags`). The classification
(§1.1/§1.2), the mixed-play discoveries, and the container-watch
register/`when:` hazard finding are reused verbatim from round 2 — not
re-litigated here except where the new mechanism changes how they're
expressed.

**Method**: same discipline as rounds 1–2 — verify against real files,
execute rather than reason where possible. This round I had
`ansible-core 2.19.11` available and used it extensively: I independently
reproduced every load-bearing empirical claim in the revision log with my
own fixtures (not a copy of Sonnet's), including a fake `systemctl` to prove
the detection layer's lookup actually does/doesn't fire, and a fixture that
mirrors the real repo's directory shape (core plays directly under
`playbooks/imports/`, optional plays under `playbooks/imports/optional/`) to
pressure-test the QA gate rewrite specifically.

**Headline result**: two BLOCKERs, both in the Check 4 rewrite (§4.1), both
reproduced empirically, both with a verified one-line-class fix. Everything
else in this round — the detection layer, the `playbook-main.yml` diff, the
fail-fast assert, the three core mixed-play edits, and the container-watch
list-`when:` fix — checked out clean under direct execution.

---

## 1. Check 4a (§4.1) against the real `playbook-main.yml` — **1 BLOCKER (different bug than I was asked to hunt), the hazard I was asked to hunt confirmed real but SHOULD-FIX severity**

### First: the specific hazard the brief asked me to pressure-test — **confirmed real**

Built the exact 4a `awk`+`case` logic (byte-identical to §4.1) into a
harness and ran it against the **real, complete post-diff
`playbook-main.yml`** (reconstructed verbatim from §3.2, all 31 imports, both
real multi-line comment blocks — the LXC/Docker-ordering one and the
claude-yolo/claude-code-ordering one — included):

```
31/31 imports correctly classified: 10 gnome, 21 general, 0 errors.
```

This confirms the proposal's own claim for the *actual* file — the existing
comment blocks sit between two general imports in both cases, never between
a gnome import and its own `when:`, so the hazard does **not** fire on the
diff as written.

Then I built the exact malformation the brief hypothesized — a comment
between an import and its intended `when:`:

```yaml
- import_playbook: imports/play-firefox.yml
# gated because it needs a display
  when: provisioning_profile != 'server'
```

Result: **`play-firefox.yml` silently recorded as `general`, the `when:`
line silently dropped, zero errors reported.** Reproduced identically with a
blank line instead of a comment. This is real, exactly as hypothesized: the
catch-all flush rule (`{ if (pending != "") { print pending "|UNGUARDED"; pending = "" } }`) fires unconditionally on the comment/blank line, clearing
`pending` *before* the `when:` line is ever reached — so the `when:` rule's
own guard (`&& pending != ""`) is false when it matters, and the line falls
through the catch-all a second time with `pending` already empty (a no-op).

**Severity**: this does not affect the round-3 diff as written (verified
above — 0 errors on the real file), so it is **not** a blocker to shipping
this round's changes. But it is a real, permanent gap: §3.1 states the
no-comment-in-between rule is "enforced by Check 4," and that's not true —
Check 4 silently accepts a violation of its own documented grammar rather
than rejecting it. This directly undercuts one of this plan's explicit
success criteria ("the QA gate **fails** on a missing... scope
declaration") — an orphaned `when:` *is* a missing declaration for that
play, and the gate reports success. **SHOULD-FIX**, high priority (it's the
exact kind of drift this gate exists to prevent, and will bite the first
future contributor who adds a rationale comment in the wrong place), not a
blocker to this round.

**Verified fix** (recommended, not yet applied by Sonnet): make the `when:`
rule match unconditionally and branch on `pending`, instead of gating the
rule itself on `pending != ""`:

```awk
/^  when: / {
    if (pending == "") {
        print "ORPHANED|" $0
        next
    }
    cond = $0
    sub(/^  when: /, "", cond)
    print pending "|" cond
    pending = ""
    next
}
```

...paired with a new `ORPHANED|...` case in the bash `case` statement that
emits an error. I built and ran this fix: it correctly flags both the
comment-between and blank-line-between hazards as errors, **and** still
produces 0 errors / 31-correct on the real diff (no regression). A smaller
side-benefit: this same fix also closes two related variants I traced by
hand but didn't need to separately fixture — two consecutive `when:` lines
after one import (second one currently silently dropped, same root cause)
and a `when:` at the wrong indent depth (currently invisible to both the
original and my fix, since neither rule's regex matches an off-grammar
indent at all) — the latter is a smaller residual gap worth a one-line
mention in the fix's own comment, not a separate finding.

### Second: a BLOCKER the brief didn't specifically name but the "run it against the real file" instruction surfaced — **4b's file discovery doesn't exclude core plays, so it flags all 31 of them**

While building a fixture that reproduces this repo's real directory shape
(core plays as direct children of `playbooks/imports/`, optional plays under
`playbooks/imports/optional/`) to test 4a, I ran the **combined** 4a+4b
script (as instructed — item 5 of the brief) and found this immediately:

```
===== RUN against CLEAN playbook-main.yml (all core plays correctly
      when:-gated, NO vars.scope anywhere in core files — as the
      proposal's own §4.2/§8 instruct) =====
  ERROR (scope): playbooks/imports/optional/common/play-missing-scope.yml - missing vars.scope
  ERROR (scope): playbooks/imports/optional/experimental/play-multi.yml — 2 plays
  ERROR (scope): playbooks/imports/play-general-one.yml - missing vars.scope
  ERROR (scope): playbooks/imports/play-gnome-one.yml - missing vars.scope
  ERROR (scope): playbooks/imports/play-server-one.yml - missing vars.scope
TOTAL ERRORS: 5
```

The three core play files — correctly `when:`-gated at the import site,
correctly carrying **no** `vars.scope` per §4.2's own design ("Optional
plays... carry an informational play-level `scope:`... CORE plays don't
need this, their classification is the import-site `when:`") — are flagged
as **"missing vars.scope"** anyway.

**Root cause**: 4b's discovery loop only excludes two things —
`$CORE_MAIN` (i.e., `playbook-main.yml` itself, by exact path match) and
anything under `/optional/archived/`. It does **not** exclude
`playbooks/imports/*.yml` (the 31 core play files). Since every core play
file has a top-level `- hosts:` line (passing 4b's playbook-discovery
guard) and is not `playbook-main.yml` and is not archived, 4b processes all
31 of them and correctly reports — by its own logic — that none of them has
a `vars.scope`. But that's not a bug in a core play; it's 4b applying an
optional-play requirement to a file the proposal's own design says is
exempt.

**This is unconditional, not an edge case.** It fires on **every** correctly
implemented core play, the moment checklist step 6/7 is followed exactly as
written. It directly contradicts checklist step 7's own claim: "pass once
every playbook (except the one archived play) carries a recognised
declaration" — as the code is actually written, `./scripts/qa-all.bash`
would **never** pass, because the 31 core plays (which the design correctly
says should carry no `vars.scope`) permanently fail 4b's check. This is
worse in kind than the orphaned-`when:` gap: that one requires a specific
future malformation to manifest; this one manifests immediately, for
everyone, on the very first `qa-all.bash` run after implementation is
"complete."

**BLOCKER.**

**Verified fix**: exclude anything not under an `/optional/` directory from
4b's scan — one line, added before (or replacing) the existing
`$CORE_MAIN`-only exclusion:

```bash
while IFS= read -r -d '' yml_file; do
    [[ "$yml_file" != */optional/* ]] && continue
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR" || continue
    [[ "$yml_file" == */optional/archived/* ]] && continue
    ...
```

I re-ran the exact same fixture with this one-line fix and confirmed: the 3
core plays are no longer flagged, the 2 genuinely-broken optional plays
still are, and — checked separately — a realistic multi-var `vars:` block
(mirroring `play-container-watch.yml`'s real shape, `scope:` as the *last*
of five vars entries, not the first) is still correctly recognised. I also
re-ran the fixed script against a **bad-`when:`** core file and an
**empty-imports** core file (both scenarios the brief specifically asked
for) — both produced exactly the expected error count with no crash, no
false positive, no false negative:

```
bad when:    3 errors (1 core invalid-when + the same 2 pre-existing optional violations)
empty core:  2 errors (0 core violations, same 2 optional violations)
```

---

## 2. The `group_vars/desktop.yml` detection layer (§2) — **verified accurate on every claim, no finding**

Built an independent fixture (not copying Sonnet's) with a **fake
`systemctl`** on `PATH` that touches a marker file when called with
`get-default`, wired into the exact §2.1 file content verbatim
(`_systemd_default_target` / `provisioning_profile` templates, unchanged).
Ran all four modes:

| Mode                                       | Marker created (lookup ran)? | Gnome task                                                                                                                                                                                                 |
| ------------------------------------------ | ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--syntax-check`                           | No                           | n/a                                                                                                                                                                                                        |
| `--list-tasks`                             | No                           | listed under every profile (doesn't evaluate `when:`)                                                                                                                                                      |
| real run, no `-e`                          | **Yes**                      | ran (`skipped=0`)                                                                                                                                                                                          |
| real run, `-e provisioning_profile=server` | **No**                       | `skipping: [localhost]` ×2 (both auto-inserted "Gathering Facts" and the explicit task — expected, `when:` on `import_playbook` propagates to every task in the imported play, including the implicit one) |

Matches every claim in §2.2 exactly. Confirms: (a) inert during both
container-safe static checks, (b) self-configures correctly on a real run
with zero flags, (c) the override genuinely skips the lookup (not just
"produces the right answer despite running it").

**Inventory-layout claims, independently checked against the real repo, not
just Sonnet's assertion**:

- `ansible.cfg`'s `inventory = ./environment/localhost` — confirmed, so
  `environment/localhost/group_vars/desktop.yml` is exactly where Ansible's
  standard directory-inventory convention looks for group vars.
- `environment/localhost/host_vars/localhost.yml` exists today — confirmed,
  so the "direct sibling of an established, working pattern" claim is not
  invented precedent.
- `environment/localhost/hosts.yml`'s real content: `desktop: hosts: localhost: ...` — confirmed `localhost` **is** a member of group
  `desktop`, so `group_vars/desktop.yml` genuinely applies to the host this
  repo provisions. This was the single most load-bearing fact to get wrong
  (a mismatched group name would mean `provisioning_profile` is silently
  never defined) and it's correct.

**§2.3's fail-fast assert** — built and ran the actual assert content as
the first task of the first play in my fixture:

- Runs correctly as literally the first task of the first play, resolves
  `provisioning_profile` correctly (confirmed both on the default/desktop
  path and with `-e provisioning_profile=server` — the assert is **not**
  accidentally gated; `play-AA`'s equivalent in my fixture is unguarded and
  ran under both profiles, matching the real `play-AA-preflight-sanity.yml`
  which has no `when:` on its own import).
- **Simulated group_vars failing to load** (renamed the directory away):
  the assert's own `fail_msg` templating errors immediately —
  `'provisioning_profile' is undefined` — a hard, loud Ansible error
  (`exit 2`), never a silent pass. Confirms the "would fail loudly rather
  than silently" claim precisely, and shows it fails even *harder* than a
  graceful assert failure (template-resolution error, not just a failed
  condition) — still the correct, desired outcome either way.
- **Typo'd override** (`-e provisioning_profile=srever`) — caught cleanly,
  exact `fail_msg` text, `exit 2`, `failed=1`. Confirms the guard's whole
  reason for existing is real, not just plausible: without it, `srever != 'server'` is true, so a typo would silently behave like `desktop` and run
  every `gnome`-bucket play.

The real `play-AA-preflight-sanity.yml`'s "Before" quote in §2.3 was also
diffed against the actual file — byte-identical.

No finding. This section is solid.

---

## 3. Selection for core plays (§3) — **verified accurate, no finding**

- The three canonical forms (§3.1) match this round's tested mechanism
  exactly — no new claims beyond what §1/§2 already established.
- §3.2's full "after" `playbook-main.yml` was diffed line-by-line against
  the actual current file (captured at the start of this audit session):
  the import order and every comment block match verbatim; the 10 `when:`
  lines added are exactly the 10 plays classified `gnome` in §1.1, no more,
  no fewer.
- Ran the real 4a parser (not a hand-simplified stand-in) against this exact
  reconstructed file: 31/31 correct, 0 errors (§1 above).

No finding beyond what's already covered in §1.

---

## 4. Container-watch list-form `when:` fix (§5.4) — **verified correct by direct execution, no finding**

This was the claim I was most skeptical of walking in, so I built an
independent reproduction of the *exact* register/consumer shape (a task
that `register`s a var only when `provisioning_profile != 'server'`, and a
downstream task gated on `[provisioning_profile != 'server', <that var>.rc == 0]`) — not a simplified toy, the real dependency shape:

- **Correct order (profile gate first), server profile**: producer task
  skips (never registers), consumer task **also cleanly skips**, `exit 0`,
  no undefined-variable error. Confirms Ansible's list-form `when:` really
  does short-circuit left-to-right.
- **Correct order, desktop profile**: both tasks run normally, full chain
  evaluates.
- **Wrong order (register-dependent condition first)** — I additionally
  built this to test the proposal's own warning, not just the happy path:
  `[ERROR]: Task failed: ... object of type 'dict' has no attribute 'rc'`.
  This confirms the ordering requirement is genuinely load-bearing, not
  defensive over-caution — get it backwards and it really does break.

Cross-checked §5.4's exact diff against my round-2 verification of the real
`play-container-watch.yml`: the same 4 tasks that had no pre-existing
`when:` (directory/deploy/probe-check/enable) get a plain scalar; the same 4
that already depended on a `register`ed var (disable/wait/reload-complete/
enable-deferred) get the list form with the pre-existing condition
preserved verbatim and the profile gate prepended. Matches exactly.

No finding.

---

## 5. `set -e` safety of the combined Check 4 (4a + 4b) — **confirmed safe across every branch I could construct, modulo the two findings above**

Built fixtures for: a clean core file, a clean optional file, a bad `when:`
core import, a missing-`vars.scope` optional play, a multi-play optional
file (mirroring the real `play-virtualbox-windows.yml` structurally, not
copying it), an archived play (correctly exempted in every run), and a
zero-import-lines core file. Ran the **exact combined 4a+4b script** (not
simplified) under `set -euo pipefail` against every combination:

- **Zero aborts in any scenario**, including the empty-imports edge case
  the revision log specifically calls out.
- **Correct counts in every branch** — 0/1/2+ `vars.scope` entries, present/
  absent `when:`, valid/invalid condition strings, single/multi-play files —
  all landed in the right bucket.
- The claimed reason 4a is safe "by construction" checks out: it's a pure
  `awk` state machine feeding a `case` statement, no `grep -c` anywhere in
  that loop. 4b's one `grep -c` (`n=$(... | grep -c . || true)`) is guarded
  exactly like round 2's fix, confirmed present and confirmed working
  (tested the `n -eq 0` branch directly, no abort).

The only caveats to "no crash" are the two **correctness** bugs already
filed above (§1) — neither one crashes anything; both silently produce a
wrong *result* (respectively: a false pass, and a false-positive flood).
`set -e` safety and *correctness* are different properties, and this round's
`set -e` work is genuinely solid — the bugs that remain are logic bugs, not
robustness bugs.

---

## 6. Regression / reuse integrity — **confirmed, no finding**

- §1.1's core-31 table, §1.2's mixed-concern catalogue, and §1.3's optional
  fast-pass table read as substantively identical to what I verified in
  `AUDIT-round-2.md` (same tallies — 21/10/0 core, same three core
  mixed-play discoveries, same container-watch 8-task register/`when:`
  finding, same `play-virtualbox-windows.yml` two-play finding) — only the
  gate-mechanism column changed (tag string → bucket name), as the revision
  log claims.
- §5.1–§5.3's "Before" task quotes are the same task bodies I diffed against
  the real files in rounds 1 and 2 (unchanged since — re-confirmed no drift
  by re-reading the live files for this round's §2/§3 verification work,
  which touches the same file tree).
- §5.4's 8-task enumeration and the register/`when:` hazard behind it
  (`enable_result` consumed by both the reload-complete and
  enable-deferred debug tasks) matches my round-2 hand-trace of the real
  `play-container-watch.yml` exactly — same 8 tasks, same root cause.
- No contradiction found between this round's new sections and the
  untouched round-2 material: §9's limitations list is honest about what
  changed (loss of the cheap `--list-tasks` proof, the `connection: local`
  assumption, the asymmetric-mis-detection tradeoff) but — worth noting
  plainly rather than silently — it does **not** mention either bug found in
  §1 above. That's not a contradiction, just an incompleteness the next
  revision should close by adding both to §9 once fixed (or, if fixed before
  the next round, removing the need to mention them at all).

---

## Summary

- **2 BLOCKERs**, both in the Check 4 rewrite (§4.1):
  1. **4b flags every one of the 31 core plays as "missing `vars.scope`"** —
     unconditional, fires on day one, makes `./scripts/qa-all.bash` never
     pass as the code is currently written, directly contradicts checklist
     step 7's own claim. Root cause: 4b's file discovery excludes only
     `playbook-main.yml` and archived plays, not the core `playbooks/imports/*.yml` files. Fix verified: `[[ "$yml_file" != */optional/* ]] && continue`.
  2. *(Downgraded to SHOULD-FIX on reflection — see below — but leading
     with it here since it's the hazard the brief specifically asked me to
     hunt and it IS real.)*
- **1 SHOULD-FIX** (high priority): 4a silently drops an orphaned `when:`
  line and misclassifies the preceding gnome/server play as `general` with
  zero error, if a comment or blank line ever separates an import from its
  `when:`. Confirmed real by direct execution (both comment and blank-line
  variants). Does **not** affect the round-3 diff as written (verified 31/31
  correct on the actual reconstructed file) — this is why it's SHOULD-FIX
  rather than a second BLOCKER — but it means §3.1's claim that the
  no-comment grammar rule is "enforced by Check 4" is currently false, and
  it undercuts this plan's own success criterion that the gate fails on a
  missing declaration. Fix verified: make the `when:` rule fire
  unconditionally and branch on whether `pending` is empty, rather than
  gating the rule on `pending != ""`.
- Verified clean, no findings: the detection layer end-to-end (§2, including
  the inventory-layout facts that make it load-bearing-correct), the exact
  `playbook-main.yml` diff (§3.2), the fail-fast assert (§2.3, including its
  loud-failure behaviour when group_vars is simulated missing), the
  container-watch list-`when:` short-circuit claim (§5.4, verified in both
  the correct-order and deliberately-wrong-order direction), and reuse
  integrity against round 2 (§1, §5.1–§5.3).

**Verdict for the team lead**: the comment-between-import-and-`when:` hazard
you flagged is **real** — confirmed by direct execution, not just plausible
— but it does not touch the diff this round actually ships (the real
`playbook-main.yml` has no comment sitting in that position for any of the
10 gnome imports), so I'm filing it SHOULD-FIX rather than a blocker to this
round specifically. What *does* block this round is something the "run it
against the real file" instruction surfaced on its own: **4b's optional-play
loop doesn't exclude the 31 core play files, so it flags every one of them
as missing `vars.scope`, unconditionally, from the first `qa-all.bash` run**
— this is not implementable as-is. Both fixes are small (one line each) and
I've verified both resolve their respective problem without introducing a
regression on the real file or any of my other fixtures. Recommend a round-4
revision-and-reaudit of just these two lines, not a full re-derivation.
