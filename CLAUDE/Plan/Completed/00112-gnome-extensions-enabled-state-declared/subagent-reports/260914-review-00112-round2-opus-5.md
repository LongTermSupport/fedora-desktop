# QA Review — Plan 00112, commit `688a73be` (round 2, Task 2.3)

Reviewer: qa-reviewer (Opus 5). Read-only: nothing in the reviewed tree was changed.
Scope: `git show 688a73be` read against the whole repo, plus the round-1 report
`260914-review-00112-final-opus-5.md` in this directory.

**Verdict**: FIX-BEFORE-MERGE. In the dispatch's binary: **do not tick Task 2.3 yet** — one clause short.

Both prior blocking findings are **genuinely closed**, and each was re-derived here rather than
taken on the author's account. Neither is re-blocked. The hold is one new item in the code this
commit added; it is one line, it lives in the play Task 2.1 must deploy, so it costs nothing now
and costs a host redeploy later.

---

## Blocking

### 1. The new gate passes with `COVERAGE: 0 of 0` whenever the verify loop did not run — the fix's own failure mode is the defect it was written to fix

`playbooks/imports/play-gnome-shell-extensions.yml:228,238`

`that: gse_marked | int == gse_total | int` compares two numbers *both* derived from
`gse_verify.results | default([])`. When that list is empty the comparison is `0 == 0`.
Measured through ansible-core 2.19.13's own `Templar` on the three shapes that produce it:

| `gse_verify` shape                                        | `gse_total` |
| ---------------------------------------------------------- | ----------- |
| undefined entirely (`--start-at-task` at this task)        | 0           |
| register of a skipped task (no `results` key — a `when:`)  | 0           |
| register of a skipped loop (`results: []`)                 | 0           |

All three pass the assert and print
`COVERAGE: 0 of 0 deployed extensions judged against a live session`. That reads as a completed gate.

This is not an imported standard — the play already holds itself to it. Lines 162-166 reason about
this exact hazard for the loop:

> `| default([])` on its own would then turn this repo's fail-fast verify gate into a silent
> zero-iteration no-op for any future reason the fact goes unset — a `when:`, a --tags selection,
> an edit to the applier task. This assertion is what keeps that loud.

`Assert Deployed Extension UUIDs Were Collected` does keep *that* loud, and it covers the
undefined-fact path (9 != 0 fails). What it does **not** cover is a `when:` added to the verify
task itself: `gse_deployed_uuids` is still set, the earlier assert still passes 9 == 9, the verify
task is skipped, and the new assert reports `0 of 0` and goes green. One future `when:` silently
neuters the gate with no other symptom. The play has no `tags:` anywhere, so tag selection skips
both asserts together — that path is safe.

**Fix** — one clause, anchored on the plan's single source rather than on the sibling task:

```yaml
        that:
          - gse_total | int == declared_extension_uuids | length
          - gse_marked | int == gse_total | int
```

with `fail_msg` extended to name a short `gse_total`. `gse_deployed_uuids | default([]) | length`
also works; `declared_extension_uuids` is the stronger of the two.

---

## Should fix

### 2. The `re.escape` case can go vacuous and nothing would say so

`scripts/test-secret-scan.bash:211-215`

`DOTTED_UUID` is never asserted exempt anywhere in the suite. Its near-miss is asserted *flagged* —
but an undeclared UUID's near-miss is flagged regardless, so if that extension ever leaves
`vars/gnome-shell-extensions.yml` the case keeps passing while proving nothing about `re.escape`.
Measured, running the real `hook_keep_unwhitelisted` with that entry removed from the allowlist:
the wildcard case still passes.

The three anchor cases do not have this problem, and the contrast is the argument: they derive from
`DECLARED_UUID`, which **is** asserted exempt at `:176-177`. Measured: drop that UUID from the
allowlist and the case fails loudly, so the anchor cases cannot go vacuous in silence.

**Fix**: one line beside the others —
`assert_filter "the dotted UUID is itself exempt" "" "${REPO_ROOT}" "1: ${DOTTED_UUID}"`.

---

## Answers to the four questions in the dispatch

- **Is the Jinja correct?** Yes, re-derived independently. The five expressions were copied out of
  `:238-243` and run through `Templar` on nine verdict shapes. `EXT-FAIL` **is** counted as marked
  (`EXT-(OK|FAIL) .[a-z_]+. ` gives marked=9, live=8 on a fail-among-ok shape); `pending_reload`
  **is** counted as judged-against-a-live-session (`pending_reload x3 + ok x6` gives live=9); a
  missing marker and a garbage line both **fail** (marked=8, total=9). `gse_live + gse_scan +
  gse_nosession == gse_total` holds on every non-failing mix. `.` standing in for the brackets is
  fine, and no verdict string in `extension_state.py` falls outside `[a-z_]+`. An `EXT-FAIL` cannot
  actually reach this task — the command module fails the play on rc 1 first — so counting it as
  marked is harmless correctness, not live behaviour.
- **Is `when: not ansible_check_mode` right, and can `gse_verify` be undefined or lack `stdout`?**
  The guard is right, and a `gse_verify.results is defined` guard would have been **wrong**. Under
  `--check` the command module returns a skipped result with no `stdout` key, and
  `map(attribute='stdout')` over such a result raises `AnsibleUndefinedVariable` (measured), which
  would error the task rather than skip it. `gse_verify` undefined templates to `[]` via the
  chainable-undefined path — that is blocking #1, not a crash.
- **Is treating `pending_scan` as non-failing the same "gate that cannot fail" defect one level
  up?** No — plainly, it is the right call. Failing it would break every fresh install before the
  reboot `run.bash` already requires, and the missing-marker condition genuinely fails, which was
  demonstrated. The gate is not unfailable. The real residual "cannot fail" surface is the empty
  population in blocking #1.
- **Does `success_msg` reach a reader at default verbosity?** **Yes** — the crux resolves in the
  fix's favour, established from source rather than from a run.
  `plugins/action/assert.py:77-78` sets `result['_ansible_verbose_always'] = True` whenever `quiet`
  is false; `plugins/callback/__init__.py:264-266` returns true from `_run_is_verbose` on that key
  alone; `plugins/callback/default.py:103` then appends the dumped result to the `ok:` line.
  `_clean_results` only strips for `debug`, and `_dump_results` keeps `msg`. `ansible.cfg:17` pins
  `stdout_callback = ansible.builtin.default` with `result_format = yaml`, and `run.bash` does not
  pipe or filter `ansible-playbook` output (`:699,1588-1593,2743-2748`). The COVERAGE line lands on
  the operator's terminal.

---

## Nits

- **`quiet: false` at `:236` is the single line holding the fix up, and nothing says so.** It is the
  default, so it reads as redundant boilerplate a tidying edit would delete — and `quiet: true`
  skips the `_ansible_verbose_always` assignment, which silently deletes the COVERAGE line at
  default verbosity and restores the exact reported defect. One comment fixes it:
  `# quiet: false is load-bearing — it sets _ansible_verbose_always, which is what prints success_msg without -v.`
- **`scripts/test-secret-scan.bash:197-198` contradicts the code thirteen lines below it.** "All
  three are derived from the declared UUID rather than written out, so this file grows no new
  address-shaped literal and nothing drifts if the declared set changes" — `:211` then writes out a
  new address-shaped literal that does drift, silently (finding 2). True of the three it names;
  false of the block as a whole.
- **`SUFFIXED` / `PREFIXED` at `:199-200` are named for the operation and commented for the
  result**, so they read as opposites: `SUFFIXED` is the one whose comment says "strict PREFIX".
  `UUID_WITH_TAIL` / `UUID_WITH_HEAD` would remove the double-take.
- **The commit message undercounts its own content**: "Three cases added" and "27 secret-scan tests"
  against four cases and `passed: 28`. The 12:04 journal entry already records the fourth case
  explicitly, which is the correction; the commit is pushed and should not be amended.
- **`PLAN.md:97-101` Task 1.8 still describes the pre-fix coverage.** Finding 1 got a new Task 1.10;
  finding 2 got nothing in `PLAN.md` and lives only in the journal. Not drift — 1.8's claim is true,
  just narrower than what now ships. One clause naming the anchor cases would make the ticked task
  describe what the suite proves.
- **Round 2's four nits are all still open and still unrecorded as declined.** The `validate_uuid`
  one was re-checked rather than carried: a newline in a `uuid:` is escaped by `re.escape` as
  backslash-newline, so the split always leaves a fragment ending in a backslash, grep rejects it
  with exit 2, `_hook_grep_v` returns 2, `hook_keep_unwhitelisted` returns 1, and
  `pre-commit:253-256` exits 1. Hard fail confirmed — the direction is safe, the operator message
  is still poor.

---

## Is there a further loosening all 28 cases would still pass?

Six emitter variants were run against eight inputs through the real `hook_keep_unwhitelisted`, with
the four production whitelists verbatim. Each new case pins a distinct property and none passes for
the wrong reason:

| variant                      | which cases flip to EXEMPT                                       |
| ---------------------------- | ------------------------------------------------------------------ |
| both anchors dropped         | the three anchor cases, and only those — the author's control reproduced |
| caret only dropped           | the strict-suffix case alone                                     |
| dollar only dropped          | the strict-prefix case and the deeper-domain case                |
| `re.escape` dropped          | the wildcard near-miss alone — the author's control reproduced    |
| emitted case-insensitively   | the lowercased near-miss (pre-existing case `:186-187`)          |

Two loosenings nobody wrote a case for were also checked: emitting only the domain half is caught
by the strict-suffix case, and emitting only the local half is caught by the strict-prefix case.
The one property genuinely unpinned is that **only** values under a `uuid:` key are harvested —
widening the emitter to other keys in that file would pass all 28. No value in
`vars/gnome-shell-extensions.yml` outside `uuid:` is address-shaped today, so it exempts nothing;
recorded as the remaining gap, not as a requested case.

---

## Checked and clean (measured, not assumed)

- **IaC placement.** An edit to the play that already owns the concern; no new play,
  `playbook-main.yml` untouched. Ordering is right: verify loop, then assert, then Space Bar.
  Nothing probes for state the repo declares.
- **Naming.** `Assert Every Deployed Extension Produced A Readable Verdict` says what it does; no
  colon-space-dash pattern in the unquoted name; `gse_*` matches the file's existing prefix.
- **Fail-fast.** No `failed_when` / `ignore_errors` / `FAIL-FAST-OK` added; the only `failed_when`
  in the play is the pre-existing `gse_install.rc not in [0, 2]` tightening. No `shell: |` block
  added.
- **Public-repo safety.** Every address-shaped token in the added lines is either a declared
  extension UUID (exempt, and the scanner's own allowlist covers them) or an `@example.com`
  placeholder. No home paths, IPs, hostnames or usernames anywhere in the diff. The redaction in
  the committed round-1 report is marked at its `:71-76` and changes no measurement.
- **Journal honesty (12:04).** Honest and specific. It names the half-delivery as the author's own
  miss ("I did not notice I had stopped"), both bad fixtures (the RFC 2606 reserved TLD, and the
  `untracked/` harness whose empty allowlist made the *first* case fail while the near-misses
  passed vacuously), and both control experiments including the fourth case the commit message
  omits. Nothing overstated.
- **Version bumps.** Not applicable — nothing under `files/var/local/claude-yolo/`, no Dockerfile,
  entrypoint or deployed skill. The play stays `100755` with the correct shebang.
- **Plan/doc sync.** `PLAN.md` Task 1.10 added and committed with the code; Task 2.3 correctly
  unticked; Task 2.2 correctly blocked with the harness defect attributed to Plan 00117.
  `CLAUDE/Plan/README.md:49` row present (`:39` for 00117). `docs/playbooks.md:525-560` needs no
  change — the assert alters the transcript, not the behaviour the docs describe.

Unrelated to this diff but visible in the tree: `helpers/host_health/`,
`tests/helpers/host_health/` and a round-4 report for Plan 00068 are untracked from other sessions.
A `git add -A` would sweep them into a 00112 commit.

---

## Mechanical gates

| Gate                                                              | Result                                                                                                                                                                    |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scripts/qa-all.bash`                                             | **PASS**, exit 0 — 825 files; `secret-scan-tests: passed: 28`; `helper-tests: Ran 943 tests`; `ansible-syntax: 79 playbooks OK (76 under playbooks/imports/, 3 elsewhere)` |
| `hooks-daemon plan-qa --sweep`                                    | exit 1, **0 block / 2 advise** — Plan 00046 stale path, journal-freshness on 12 older plans. Nothing against 00112; identical to prior rounds                             |
| `ansible-playbook --syntax-check play-gnome-shell-extensions.yml` | **PASS** (rc 0)                                                                                                                                                            |
| `scripts/test-secret-scan.bash`                                   | **triggered** — ran standalone, `passed: 28  failed: 0`, all four new cases green                                                                                         |
| `scripts/qa-helper-tests.bash`                                    | **not triggered** — no `helpers/` or `tests/helpers/` file in this diff. `qa-all.bash` ran it anyway: 943 tests, exit 0                                                    |
| `python3 -m helpers.gnome.check_extension_compat`                 | **not triggered** — no `extensions/**/metadata.json` in the diff. `qa-all.bash` ran it anyway: 4 extensions OK                                                             |
| `extensions` ESLint                                               | **not triggered** — no extension JS in the diff                                                                                                                            |
| shellcheck on the changed script                                  | 1 informational SC1091 at `:31`, pre-existing (the `source` of the library under test)                                                                                    |

The play was not run. Per `CLAUDE.md` this is a CCY container, so every finding above was
established from Ansible's own templating engine, its action and callback plugin source, and the
real scanner functions — no playbook execution.
