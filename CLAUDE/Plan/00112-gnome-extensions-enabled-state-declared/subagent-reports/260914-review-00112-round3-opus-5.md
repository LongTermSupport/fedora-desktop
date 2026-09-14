# QA Review — Plan 00112, commit `d13c10cf` (round 3, Task 2.3)

Reviewer: qa-reviewer (Opus 5). Read-only: nothing in the reviewed tree was changed.
Scope: `git show d13c10cf` read against the whole repo, plus the committed round-1 and
round-2 reports in this directory.

**Verdict**: PASS WITH NITS. In the dispatch's binary: **Task 2.3 is safe to tick now.**

Every measurement below was re-derived from scratch. The author's two harnesses were
scratch and are gone; I did not reuse them, and I did not take round 2's numbers on trust.

> **Redaction.** One measured string — the dotted UUID with its interior dot substituted
> for a letter — is address-shaped and correctly NOT exempt, so writing it out would block
> the commit of this report (round 1 hit exactly this). It appears below as
> `<dotted-UUID near-miss>`. Nothing about the measurement changes.

---

## Blocking

None.

---

## Round 2's two findings — both genuinely discharged

### Blocking 1 (the `0 of 0` gate) — closed

I rebuilt the harness independently: PyYAML reads `that` / `vars` / `fail_msg` /
`success_msg` out of `playbooks/imports/play-gnome-shell-extensions.yml:225-254` and the
vars file, and ansible-core 2.19.13's own `Templar` evaluates them. Templates must be
tagged `TrustedAsTemplate` under 2.19's data-tagging — an untagged string returns itself
verbatim, which silently makes every condition a truthy string and every shape "pass".
Worth knowing if anyone rebuilds this.

| shape                                     | `gse_total` / declared | verdict now                                              | old single clause |
| ----------------------------------------- | ---------------------- | -------------------------------------------------------- | ----------------- |
| all healthy                               | 9 / 9                  | PASS — `COVERAGE: 9 of 9`                                | pass              |
| fresh install, all `pending_scan`         | 9 / 9                  | PASS — `COVERAGE: 0 of 9 … 9 awaiting a shell rescan`    | pass              |
| mixed `pending_reload` + `skip_no_session` | 9 / 9                 | PASS — `COVERAGE: 8 of 9 … 1 with no session reachable`  | pass              |
| one unreadable verdict                    | 9 / 9, marked 8        | **FAIL**                                                 | fail              |
| short loop (8 of 9)                       | 8 / 9                  | **FAIL**                                                 | *pass*            |
| `results: []`                             | 0 / 9                  | **FAIL**                                                 | *pass*            |
| register with no `results` key (a `when:`) | 0 / 9                 | **FAIL**                                                 | *pass*            |
| `gse_verify` undefined                    | 0 / 9                  | **FAIL**                                                 | *pass*            |
| nothing declared                          | 0 / 0                  | PASS — `COVERAGE: 0 of 0`                                | pass              |
| 10 results, 9 declared                    | 10 / 9                 | **FAIL**                                                 | *pass*            |

Four shapes that used to pass now fail, exactly as the commit message claims — five
counting the over-count it does not claim.

**Is `declared_extension_uuids` shrinkable?** I probed the malformed shapes of
`vars/gnome-shell-extensions.yml` through the same Templar:

| vars-file shape        | result                                                      |
| ---------------------- | ----------------------------------------------------------- |
| a group set to `null`  | hard error — `sum` filter: cannot concatenate list and None |
| an entry missing `uuid:` | hard error — object of type dict has no attribute uuid    |
| the whole key null     | hard error — NoneType has no attribute values               |
| a DUPLICATE group key  | bare PyYAML silently keeps the last (4 uuids → 2)           |

The duplicate-key path is the only silent one, and `ansible.cfg` sets
`DUPLICATE_YAML_DICT_KEY = error` (confirmed via `ansible-config dump`), so the play fails
at vars-load. The author's reasoning holds: declared=0 is reachable only from a vars file
with genuinely empty groups — tracked repo content, visible in a diff. The residual is
structural rather than a hole: the gate is *by construction* relative to the declaration,
and the `COVERAGE: n of m` line is the disclosure. Note that adding a bare `> 0` guard
would be the **wrong** fix — that is precisely the "guard the empty case, miss the
partial" shape in `CLAUDE/AgentNotes.md`.

**`fail_msg` in both directions** — correct. Short population: "Verified 8 of 9 declared
extensions, 8 of which produced an EXT-OK/EXT-FAIL marker." Short marker count: "Verified
9 of 9 declared extensions, 8 of which …". See nit 3 for the third direction.

**`quiet: false` comment — accurate.** `plugins/action/assert.py:66` (`quiet` default
`False`), `:77-78` sets `result['_ansible_verbose_always'] = True` when not quiet, `:92`
sets `result['msg'] = success_msg`; `plugins/callback/__init__.py:265` returns true from
`_run_is_verbose` on that key alone; `plugins/callback/default.py:103-105` appends the
dumped result to the `ok:` line. Both factual claims in the comment are true.

### Should-fix 2 (the vacuous `re.escape` case) — closed, control reproduced

Sourcing the real `scripts/git-hooks/lib/secret-scan.bash` and feeding
`hook_keep_unwhitelisted` the four production whitelists plus a doctored derived list
(no files written):

| allowlist variant       | dotted-UUID-exempt        | wildcard near-miss        | tail       | head       | embedded   |
| ----------------------- | ------------------------- | ------------------------- | ---------- | ---------- | ---------- |
| production              | EXEMPT ✔                  | FLAGGED ✔                 | FLAGGED    | FLAGGED    | FLAGGED    |
| dotted UUID removed     | **FLAGGED — case fails**  | FLAGGED — passes vacuously | –         | –          | –          |
| `re.escape` dropped     | EXEMPT                    | **EXEMPT — case fails**   | –          | –          | –          |
| caret dropped only      | –                         | –                         | FLAGGED    | **EXEMPT** | FLAGGED    |
| dollar dropped only     | –                         | –                         | **EXEMPT** | FLAGGED    | **EXEMPT** |

So the new assertion does fail when the UUID leaves the allowlist; it is the only case
that does (the commit message's "exactly one of 29" is corroborated); and it changes no
existing case's verdict — the production row matches every verdict the suite asserts.
`scripts/test-secret-scan.bash` standalone: `passed: 29  failed: 0`.

**Renamed fixtures** — `UUID_WITH_TAIL` / `UUID_WITH_HEAD` at
`scripts/test-secret-scan.bash:200-201`; both use sites updated (`:227,229`), the labels
still match the semantics, and `grep -rn 'SUFFIXED\|PREFIXED'` finds nothing outside the
archived round-1/2 reports.

**`PLAN.md` Task 1.8's new wording is true, not merely longer.** "each is held by a case
that fails if only that property is dropped" — caret is held by uuid-with-head, dollar by
uuid-with-tail and embedded, `re.escape` by the wildcard near-miss (table above).

---

## Should fix

### 1. Round 1's three live nits are three rounds old and recorded nowhere

`grep -rn "validate_uuid\|worked example\|spelled twice\|declined"` across `PLAN.md` and
`JOURNAL/` returns nothing. The repo's practice is fix-or-record; a carried nit with no
recorded decision is indistinguishable from a forgotten one. All three are still live:

- `scripts/git-hooks/lib/secret-scan.bash:105-106` cites `Vitals@CoreCoding.com` and
  `clipboard-indicator@tudmotu.com` as its own worked examples — the scanner's source is
  committable only because of the file it reads.
- `hook_extension_uuid_allowlist` (`:114-145`) still does not apply
  `enabled_extensions.validate_uuid`, which exists at
  `helpers/gnome/enabled_extensions.py:147`. Direction is safe (hard fail); the operator
  message is not.
- `vars/gnome-shell-extensions.yml:70` and
  `playbooks/imports/play-gnome-shell-extensions.yml:114-115` spell the custom UUID twice,
  against the vars header's "no consumer keeps a copy".

`PLAN.md` is being edited anyway to tick Task 2.3 — one line each costs nothing.

---

## Nits

1. **`scripts/test-secret-scan.bash:195` overstates what its cases prove**: "Each case
   below fails if either anchor is removed." Measured: dropping only the caret fails
   `uuid-with-head` alone; dropping only the dollar fails `uuid-with-tail` and `embedded`.
   The true statement is the one `PLAN.md` now makes — each anchor is held by at least one
   case.
2. **`:214` "the three above have no interior dot to substitute"** — `Vitals@CoreCoding.com`
   does have one. The operative property is that substituting it leaves a string the email
   pattern no longer matches (measured), whereas `<dotted-UUID near-miss>` still matches.
   "no interior dot whose substitution leaves an address-shaped string" would be exact.
3. **The `fail_msg` explains only the short direction.** On the over-count shape it prints
   "Verified 10 of 9 declared extensions" followed by two sentences about *short* counts.
   Unreachable while `Assert Deployed Extension UUIDs Were Collected` (`:167-177`) stands,
   since both asserts carry the same `when:` — cosmetic only.
4. **`gse_total` is a denominator in one message and a numerator in the other** (`:235`
   "Verified {{ gse_total }} of {{ declared… }}" vs `:241` "{{ gse_live }} of
   {{ gse_total }}"). Coherent, because the success path renders only once they are equal,
   but `gse_verified` / `gse_results` would read straight.
5. **`docs/playbooks.md` does not mention the COVERAGE line for this play**, while `:139`
   documents exactly that for a sibling play. Nothing in the docs is now false, so this is
   consistency rather than drift.

---

## Checked and clean (measured, not assumed)

- **Marker parsing covers the whole population.** `helpers/gnome/verify_extension.py:155-156`
  prints `EXT-OK|EXT-FAIL [<verdict>] <message>`; all six `Verdict` values in
  `extension_state.py:28-39` are `[a-z_]+`, so `gse_marked`'s regex cannot under-match.
  `gse_live` is anchored (`select('match')`); `gse_scan` / `gse_nosession` use `search`,
  and every message literal in `classify()` (`:66-109`) was read — none contains another
  verdict's token, so the three-way breakdown sums to the total.
- **Membership, not just length.** `apply_enabled_extensions.py:98-111` builds the marker
  from `resolution.found`, a subset of the `--uuid=` args (the declared list), with
  `resolution.missing` hard-failing first. `|found| == |declared|` therefore implies set
  equality; a set diff would buy nothing here.
- **IaC placement.** An edit to the play that already owns the concern. No new play;
  `playbooks/playbook-main.yml:43` untouched (last changed by Plan 00115). Ordering
  unchanged: collect → assert → verify loop → coverage assert → Space Bar. No runtime
  probing for state the repo declares.
- **Fail-fast.** No `failed_when` / `ignore_errors` / `FAIL-FAST-OK` added; no `shell: |`
  block added. The diff strictly tightens a gate.
- **Public-repo safety.** The only address-shaped literals in the added lines are
  `${DOTTED_UUID}` interpolations; `DOTTED_UUID` itself is a context line from the prior
  commit and is a declared, exempt UUID. The committed round-2 report contains zero
  address-shaped tokens (grepped). No home paths, IPs, hostnames or usernames in the diff.
- **Version bumps.** Not applicable — nothing under `files/var/local/claude-yolo/`, no
  Dockerfile, entrypoint or deployed skill. The play stays `100755` with the correct
  shebang.
- **Plan/code sync.** Plan, journal and code landed in one commit; Task 1.10's sub-bullets
  match what I measured shape-for-shape; Task 2.3 correctly unticked; Task 2.2 correctly
  blocked against Plan 00117. The journal entry is appended, not edited. Branch reads
  `## F44...origin/F44` — in sync, nothing unpushed. The only untracked path is the
  unrelated Plan 00068 report, which this commit did not sweep in.
- **Commit-message honesty.** "Four of those five used to pass", "eight shapes", "exactly
  one of 29 cases fails" — all three reproduced independently.

---

## Mechanical gates

| Gate                                                              | Result                                                                                                                                                                    |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `./scripts/qa-all.bash`                                           | **PASS**, exit 0 — 827 files; `secret-scan-tests: passed: 29`; `helper-tests: Ran 969 tests`; `ansible-syntax: 79 playbooks OK (76 under playbooks/imports/, 3 elsewhere)` |
| `hooks-daemon plan-qa --sweep`                                    | exit 1, **0 block / 2 advise** — Plan 00046 stale path, journal-freshness on 12 older plans. Nothing against 00112; identical to rounds 1 and 2                            |
| `ansible-playbook --syntax-check play-gnome-shell-extensions.yml` | **PASS** (rc 0; needs a pipe in this environment, as Ansible rejects the agent shell's non-blocking stdio)                                                                 |
| `scripts/test-secret-scan.bash`                                   | **triggered** — standalone `passed: 29  failed: 0`                                                                                                                        |
| `scripts/qa-helper-tests.bash`                                    | **not triggered** — no `helpers/` or `tests/helpers/` file in the diff. `qa-all.bash` ran it anyway: 969 tests, exit 0                                                     |
| `python3 -m helpers.gnome.check_extension_compat`                 | **not triggered** — no `extensions/**/metadata.json` in the diff. `qa-all.bash` ran it anyway: 4 extensions OK                                                             |
| `extensions` ESLint                                               | **not triggered** — no extension JS in the diff                                                                                                                            |
| shellcheck on `scripts/test-secret-scan.bash`                     | 1 informational SC1091 at `:31` (the `source` of the library under test), pre-existing; clean under `-x`                                                                   |

No playbook was run — per `CLAUDE.md` this is a CCY container. Every finding was
established from Ansible's own templating engine, its action and callback plugin source,
and the real scanner functions.

**The decision**: tick Task 2.3. Nothing in `d13c10cf` needs a host redeploy to fix, and
the only non-cosmetic item is recording a decision on the three round-1 nits — a `PLAN.md`
edit that is happening anyway. Task 2.2 stays blocked on Plan 00117 and did not enter this
judgement.
