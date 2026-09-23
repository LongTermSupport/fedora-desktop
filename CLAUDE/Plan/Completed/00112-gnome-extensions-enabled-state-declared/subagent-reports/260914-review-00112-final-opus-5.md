# QA Review — Plan 00112, `cb88ec4e..ade40aba` (final pass, Task 2.3)

Reviewer: qa-reviewer (Opus 5). Read-only: nothing in the reviewed tree was changed.
Scope: the plan's two implementation commits `d307ed28` and `ade40aba`, read against the
whole repo and against both predecessor reports in `untracked/agent-reports/`.

**Verdict**: BLOCK — two items, both small and specific.

Nothing here leaks, loses data, or breaks another user; the code is otherwise sound and
both prior rounds' findings are genuinely closed. But one of round 2's two-part findings
was half-delivered with no recorded decision, and the security gate's load-bearing
invariant is untested in a way I measured.

---

## Blocking

### 1. Round 2's finding #1 was half-delivered: the verify gate still cannot distinguish nine judged extensions from nine unjudged ones

`playbooks/imports/play-gnome-shell-extensions.yml:188-211`, `helpers/gnome/extension_state.py:52-53`

The verdict split is real and correct — I confirmed the mechanism against upstream source,
not by reading the comment. `gnome-extensions list` calls `get_shell_proxy()` and returns
exit 2 with "Failed to connect to GNOME Shell" when the shell is absent
(`subprojects/extensions-tool/src/command-list.c:54-56,219`), and `gnome-extensions info`
exits 2 both for that **and** for `Extension "%s" doesn't exist`
(`command-info.c:38-40,60-61`). So `_session_available()` is a genuine independent probe
and `PENDING_SCAN` is a genuine separate verdict. That half is done.

What was not done is the second half: *make a pass say what it checked*.

- `PENDING_SCAN` and `OK` both return `exit_code` 0 (`extension_state.py:52-53`).
- `Verify Deployed Extensions Are Healthy` registers `gse_verify` (`play:203`) and
  **nothing reads it**. There is no post-loop assert; the next task is
  `Configure Space Bar`.
- Ansible does not print a `command` task's stdout without `-v`.

So on the exact scenario this plan exists for — a fresh install where the running shell
has not rescanned `~/.local/share/gnome-shell/extensions` — all nine iterations return
`EXT-OK [pending_scan]` and the play transcript is byte-identical to a genuine all-clear.
That is the AgentNotes shape "a gate whose only visible output is a failure is
indistinguishable from a gate that is not running".

This is not a judgement call about scope. The commit message for `ade40aba` enumerates the
other five round-2 items it fixed and is silent on this one, and nothing in `PLAN.md` or
the journal records declining it — I grepped the whole plan folder for "coverage" and the
only hits are the *guest* check's line and the two acceptance runs. Meanwhile
`PLAN.md:102-106` Task 1.9 is ticked ✅ and asserts that conflating the two verdicts
"let a nine-iteration gate report OK having judged nothing", implying it no longer can. At
play level it still can.

**Fix** — either:

- consume the verdicts: an assert after the loop over
  `gse_verify.results | map(attribute='stdout')` counting `[ok]` / `[pending_reload]`
  against the loop length, emitting `COVERAGE: n of m judged against a live session`; or
- amend Task 1.9 to state plainly that the distinction is helper-internal, that the
  play-level gate deliberately treats `PENDING_SCAN` as a pass, and that the post-reboot
  acceptance check is the gate that proves the plan.

Either is fine. Leaving a ticked task claiming a property the play does not have is not.

### 2. The secret scanner's anchoring is load-bearing, documented as load-bearing, and untested — I measured that the 24-case suite ships green without it

`scripts/git-hooks/lib/secret-scan.bash:140-143`, `scripts/test-secret-scan.bash:176-212`

The emitter prints `"^" + re.escape(uuid) + "$"` under a comment saying that is precisely
what stops "a real address riding along on a UUID's coat-tails". I ran the current code and
a loosened variant (identical emitter, anchors dropped) side by side:

> **Redacted on commit.** The three near-miss rows below were written out in full by the
> reviewer and are address-shaped strings that are *correctly* not exempt — so the
> pre-commit scanner rejected this report, which is the gate behaving exactly as the finding
> describes. `<UUID>` stands for the declared UUID `Vitals@CoreCoding.com`, which is exempt
> and may appear verbatim. Nothing about the measurement changes.

| input                             | current     | anchors dropped |
| --------------------------------- | ----------- | --------------- |
| `Vitals@CoreCoding.com`           | exempt      | exempt          |
| `<UUID>` + `pany` (strict prefix) | **flagged** | **exempt**      |
| `x` + `<UUID>` (strict suffix)    | **flagged** | exempt          |
| `<UUID>` + `.evil.net`            | **flagged** | exempt          |
| a real address at a live TLD      | flagged     | flagged         |
| UUID + real address on one line   | flagged     | flagged         |
| the UUID lowercased (case)        | flagged     | flagged         |

The first of those three is a plausible real address that has a declared UUID as a
strict prefix. Every one of the suite's six `assert_filter` cases, plus the
malformed-root hard-fail and the absent-root case, behaves **identically** under the
loosening — so `test-secret-scan.bash` still reports `passed: 24` and `qa-all.bash` still
exits 0.

The suite proves the exemption *works*. It does not prove the exemption is *tight*, which
is the only property that matters in this file. This is the one file in the repo where a
silent regression is a leak rather than a bug.

**Fix**: two cases — a token strictly containing a declared UUID (`<UUID>` + `pany`) and one
strictly contained by one (`x` + `<UUID>`), both asserted **flagged**. A third for
`re.escape` (a `.`-as-wildcard near-match, the UUID with its dot replaced by any letter)
closes the other half of line 143. *(Redacted as above.)*

---

## Nits (all carried over from round 2, none recorded as declined)

- `secret-scan.bash:105-106` still cites `Vitals@CoreCoding.com` and
  `clipboard-indicator@tudmotu.com` as its own worked examples. The scanner's source is
  committable only because of the file it reads; drop either extension and the comment
  blocks the commit that drops it. `something@example.com` would be better.
- `hook_extension_uuid_allowlist` still does not apply `enabled_extensions.validate_uuid`.
  A newline in a `uuid:` yields a broken ERE and the operator sees
  `grep exit 2: ^ok@fine\.com\` rather than a named error. Direction is safe (hard fail),
  message is not. This commit *added* the validator two files away.
- The custom extension's UUID is spelled twice — `vars/gnome-shell-extensions.yml:70` and
  the `src`/`dest` of `play:113-116`. The vars header says no consumer keeps a copy; this
  one does. Renaming it there leaves the copy deploying the old directory (loud, via
  `declared-extension-not-deployed`, but still a second copy).
- `ade40aba`'s commit message says "TestCheckRequired restored"; the restored class is
  `TestMissingRequired` at `tests/helpers/gnome/test_enabled_extensions.py:290`. Substance
  is present and in the right file; the name in the message is wrong.
- `guest-acceptance-desktop.bash:223` — the `expected -eq 0` floor is reachable only via a
  declared `uuid:` that is the empty string (the Python reader raises for every other empty
  case). Insurance rather than dead code; keep it.

---

## Checked and clean (measured, not assumed)

- **Single source, three consumers, every group.** All three iterate `.values()`:
  play `:21-22`, guest checker `:192`, scanner `:135`. No second copy anywhere. Running the
  guest checker's own extractor against the vars file yields **9** UUIDs in declaration
  order, so the repo's current version of the script would report
  `COVERAGE: 9 of 9 declared ACTIVE` once deployed. `expected` derives from that same list.
- **Check-mode safety (question 4).** Confirmed from
  `ansible/executor/task_executor.py`: the conditional returns
  `skipped=True, skip_reason='Conditional result was False'` at `:498`, and
  `self._task.post_validate(templar=templar)` is at `:533` — so
  `when: not ansible_check_mode` on `Collect Deployed Extension UUIDs` prevents the
  `set_fact` expression from ever being templated. `_get_loop_items()` at `:93` runs before
  that, confirming the play's comment that `loop` is templated before `when` and that the
  verify task therefore cannot carry the guard. The current form is check-mode safe.
- **The argv construction actually evaluates.** No run has exercised
  `.values() | sum(start=[])` at runtime (the third acceptance run against `be73d3b0`, of
  which `ade40aba` is an ancestor, has no recorded result). I evaluated it through
  ansible-core 2.19.13's own `Templar` with `trust_as_template`: a native `list` of 9, and
  the full `argv:` concatenation renders to 16 correct items with `--uuid=<value>` per
  extension. `DEFAULT_JINJA2_NATIVE(default) = True` confirmed from `ansible-config dump`.
  I also ran the applier with the `--uuid=` form against an empty directory: it prints
  `GNOME-EXT-FAIL declared-extension-not-deployed` and exits 1 before any gsettings call.
- **A refused write genuinely fails (question 5).** `test_a_write_that_did_not_take_is_a_failure`
  drives production `main()` with a stale read-back — not a re-implementation.
  `TestMissingRequired` restores direct coverage of the predicate, and
  `test_schema_and_key_are_overridable` now captures every gsettings call and asserts
  `--key`, `--schema` and `--disable-key`.
- **Fail-fast (question 6).** The only `failed_when` in the play is
  `gse_install.rc not in [0, 2]` — a *tightening* on the install task, pre-existing. No
  `ignore_errors`, no `FAIL-FAST-OK` anywhere on the enable path, no `|| true`,
  `2>/dev/null` or bare `except` in the added Python or Bash. Every `subprocess.run` passes
  an explicit `check=`; the three `check=False` calls each inspect the returncode on the
  following line with a comment saying why.
- **IaC placement.** No new play; the DNF split, the applier and the verify loop are edits
  to the play that already owns the concern. `vars_files` on a `vars/`-rooted file matches
  existing practice. Ordering install → compile schemas → copy custom → declare → collect →
  assert → verify → dconf is correct. `playbook-main.yml` untouched. No runtime probing for
  state the repo declares.
- **Public-repo safety (question 7).** I scanned every added line in scope. Address-shaped
  tokens are the nine upstream UUIDs, `@example.com` fixtures, synthetic `a@x`-style tokens,
  and the deliberately split `someone@corp.` + `internal`. Home paths are `{{ user_login }}`
  or `~`. No IPs, hostnames, usernames or real personal addresses. `fedora-desktop` is the
  permitted self-reference.
- **Plan/doc sync.** Working tree clean, no untracked plan directory,
  `CLAUDE/Plan/README.md:47` row present (and `:37` for 00117).
  `docs/playbooks.md:536-557` matches the shipped behaviour in both directions. Task 2.2 is
  correctly 🚫 with the harness defect attributed to Plan 00117 and neither passing run
  credited. The journal's correction entries are new entries, not edits.
- **Arithmetic.** 16 distinct check names in the guest script = `PLANNED=16` =
  `vars/vm-test-scenarios.yml:116 planned: 16`, `max_skipped: 0`.
  `deployed-extensions-active` emits exactly one `check` on all three arms. The renamed
  `declared_extensions` evidence key has no named consumer —
  `helpers/vmtest/transcript.py:32` parses keys generically.
- **Version bumps.** Not applicable — nothing under `files/var/local/claude-yolo/`, no
  Dockerfile, entrypoint or deployed-skill change. The play is `100755` with the correct
  shebang; `apply_enabled_extensions.py` and `verify_extension.py` are `100755`.
- **Ansible 2.19 traps.** `--syntax-check` passes; no `: -x` in an unquoted task name; no
  self-defaulting var; no `shell: |` block added.

---

## Mechanical gates

| Gate                                                              | Result                                                                                                                                                                                       |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scripts/qa-all.bash`                                             | **PASS**, exit 0 — 819 files; `helper-tests: Ran 913 tests`; `secret-scan-tests: passed: 24`; `ansible-syntax: 79 playbooks OK (76 under playbooks/imports/, 3 elsewhere)`                   |
| `hooks-daemon plan-qa --sweep`                                    | exit 1, **0 block / 2 advise** — a stale path in Plan 00046 and journal-freshness across older plans. Nothing against 00112; identical to both prior rounds                                  |
| `ansible-playbook --syntax-check play-gnome-shell-extensions.yml` | **PASS** (rc 0)                                                                                                                                                                              |
| `scripts/qa-helper-tests.bash`                                    | **triggered** (`helpers/` + `tests/helpers/` changed) — ran standalone, 913 tests, 1 skipped, exit 0                                                                                         |
| `scripts/test-secret-scan.bash`                                   | **triggered** (`scripts/git-hooks/lib/secret-scan.bash` changed) — ran standalone, `passed: 24  failed: 0`. See blocking #2: the count is real but the anchoring invariant is not among them |
| `python3 -m helpers.gnome.check_extension_compat`                 | **not triggered** — no `extensions/**/metadata.json` in the diff. `qa-all.bash` ran it anyway: 4 extensions OK                                                                               |
| `extensions` ESLint                                               | **not triggered** — no extension JS in the diff                                                                                                                                              |
| shellcheck                                                        | 2 informational SC1091 in changed files, both pre-existing `. /etc/os-release` lines (`guest-acceptance-desktop.bash:70,238`)                                                                |

I did not judge any acceptance run as certifying this plan, per the dispatch. Neither
blocking finding would surface in one: on a post-reboot session every extension is scanned,
so `PENDING_SCAN` never fires there, and the vars file is well-formed.
