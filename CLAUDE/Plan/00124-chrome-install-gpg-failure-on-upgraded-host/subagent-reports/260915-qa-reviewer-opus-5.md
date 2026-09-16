# QA Review — Plan 00124, full diff (`git diff 31191b5f^..HEAD`, scoped)

**Verdict**: FIX-BEFORE-MERGE

Nothing is BLOCK-level: the destructive path is sound, there is no fail-fast
violation, no leak, no data loss. Two findings falsify claims the plan and the
play make, and one of them will make Task 4.2 fail as written.

## Findings, ranked

### 1 — FIX-BEFORE-MERGE. Chrome's own `%post` overwrites the repo file this play writes, on every install and every upgrade

`playbooks/imports/play-browsers.yml:157-173`

Grounded in Chromium's packaging source, not inferred:

- `chrome/installer/linux/rpm/chrome.spec.template:128-148` creates
  `/etc/default/google-chrome` with `repo_add_once="true"` **if absent**, sources
  it, and calls `install_yum`.
- `chrome/installer/linux/common/rpm.include` — `install_yum()` unconditionally
  does `cat > /etc/yum.repos.d/google-chrome.repo` containing
  `gpgkey=https://dl.google.com/linux/linux_signing_key.pub`.
- Nothing in the rpm path ever flips `repo_add_once` to `false`. The two
  `repo_add_once` references in `common/postinst.include` belong to an unrelated
  management-service function.

Two consequences, both real:

- **Idempotence.** Failure scenario: a host with no `/etc/default/google-chrome`
  — a fresh F44 install, or any host where Chrome has never been installed. Run 1
  installs `google-chrome-stable`; its `%post` rewrites the repo file. Run 2's
  `Add Google Chrome Repository` reports **changed** (`gpgkey` differs), and does
  so again after every Chrome upgrade. That is Task 4.2's exact scenario.
- **The comment at `:164-167` is false in that state.** It says the `file://`
  gpgkey means "dnf cannot end up validating against a different fetch of the key
  than the one this play reasoned about". After the scriptlet runs, dnf validates
  against a fresh network fetch — the thing the line claims to prevent.

Fix, IaC and one task: write `/etc/default/google-chrome` with
`repo_add_once="false"` **before** the `dnf` task. The `%post` only creates that
file when absent and only calls `install_yum` when it reads `true`, so the play
then owns the repo file outright.

### 2 — FIX-BEFORE-MERGE. The triage tells the operator to misread the one task Task 4.2 turns on

`CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/probe-chrome.bash:415-417`

> "The fetch task re-downloads only when the file is absent, so this does not by
> itself mean that task reports changed."

False. `get_url` takes the "already exists, done" early exit only when a
`checksum:` is set (`ansible/modules/get_url.py:605-619` — both guards require a
non-empty `checksum`, and this task sets none). Without one it falls through to
`get_url.py:621-624`, derives `last_mod_time` from the dest mtime, and sends
`If-Modified-Since` (`ansible/module_utils/urls.py:910-912`). A 200 response
replaces the file and reports **changed**; a 304 reports ok.

Failure scenario: Google republishes `linux_signing_key.pub` (it has added three
subkeys beyond the five now expired). The operator's Task 4.2 run sees the sha256
differ, reads this line, expects `ok`, and mis-attributes the `changed` fetch task
as a regression — when it is the mechanism self-healing correctly. Reword to: the
fetch is a conditional GET, so a differing file means this task will report
`changed` on the next run, and that is the refresh working.

### 3 — Should fix. `gnupg2` declared inline in two plays where the repo already has an idiom for shared tool deps

`playbooks/imports/play-browsers.yml:82-90`,
`playbooks/imports/optional/hardware-specific/play-nvidia.yml:91-99`

`tasks/ensure-jq.yml` is included by three plays for exactly this. Two
near-identical declarations plus a duplicated five-line comment is the shape that
drifts — and they already differ (`when: nvidia_install_cuda …` in one, none in
the other; `package` here vs `dnf` in `ensure-jq.yml`). Use
`tasks/ensure-gnupg2.yml` with the `when:` on the include site in play-nvidia.

Coverage itself is complete and correct: `rpm_key` appears in exactly those two
plays; `play-vscode.yml:30` uses a raw `rpm --import`, which needs no gpg2.

### 4 — Should fix (low). `--check` fails the play instead of reporting, on a host that needs a refresh

`playbooks/imports/play-browsers.yml:141-155`

`Decide` and `Verify` both carry `check_mode: false` so they run for real; the
erase is a `command` (skipped under check) and `rpm_key` is check-mode aware
(will not import). Verify then finds `refresh` and fails the play. `run.bash:882`
documents passing extra flags such as `--check` through to `ansible-playbook`, so
this is a reachable user path. Add `when: not ansible_check_mode` to the Verify
task.

### 5 — Nit. The Task-4.2 prediction table names a task that does not exist

`probe-chrome.bash:320` says `Install The OpenPGP Tool …`; the play's task is
`Install gnupg2 for OpenPGP Key Handling` (`play-browsers.yml:87`). An operator
matching the table against run output will not find the row. The same table omits
`Add Google Chrome Repository`, which is the task finding #1 predicts will be
`changed`.

### 6 — Nit. The marker literals are the play's parsing contract and no test pins them

`helpers/rpm_keys/subkeys.py:56-57`

Measured by mutation: renaming `ACTION_MARKER` or `ENVELOPE_MARKER` leaves **all
40 tests green**, because every test reads the constant rather than the literal
the playbook greps for. Production still catches it loudly — `failed_when` at
`play-browsers.yml:115` for ACTION, and the Verify at `:153-155` for ENVELOPE (an
empty erase loop leaves the action at `refresh`) — which is why this is a nit and
not worse. Assert the literal strings in one test, or better, grep them out of
`play-browsers.yml`.

### 7 — Nit. An absent key file is reported as `COMMAND FAILED` by the script whose stated purpose is not doing that

`probe-chrome.bash:381-382` uses `probe` (every non-zero is a failure) for
`stat "${localKey}"`; `stat` exits 1 for "no such file", which in this script's
own taxonomy is a finding, not a broken command. `probe_match` is the right
helper. Low impact — the following `key_facts` probe does call `note_unanswered`,
so the run still exits non-zero. The journal at
`JOURNAL/00124-Journal-26-09-15.md:283-285` records this output and reads it as
intended behaviour.

### 8 — Nit. Naming

`Install gnupg2 for OpenPGP Key Handling` (`play-browsers.yml:87`,
`play-nvidia.yml:95`). "Handling" is filler. Say what it is for:
`Install gnupg2 (rpm_key and the key check both shell out to gpg)`.

### Observation, nothing to change

The Verify task asks whether the armour rpm stores as `%{description}` names the
subkey; dnf's actual verification is a different path. Task 4.1 confirmed they
agreed once on the real host, and `Install Google Chrome` at `:169` is itself the
production-path check in the same run — so a disagreement fails immediately
rather than silently.

## Checked and clean

- **The destructive path — clean, and both previously-broken claims genuinely
  hold.** `stale` can only contain envelopes whose parsed primary equals the
  published primary (`subkeys.py:247, 270, 280`); `report()` emits envelope lines
  only from `verdict.stale` (`:298`). Ansible's `match` test is `re.match`
  (`plugins/test/core.py:182`, `match_type='match'`), so the loop selector is
  anchored; envelope strings are rpm-generated and reach rpm via `argv:`, so
  there is no injection surface; `--allmatches` on a fully-qualified N-V-R cannot
  widen past identical NVRs. Verified by mutation rather than by reading:
  re-introducing the multi-certificate accumulation turns
  `test_a_second_certificate_does_not_donate_its_subkeys_to_the_first` red;
  deriving the erase set from `installed` instead of `ours` turns two tests red;
  reading only `envelopes[0]` turns
  `test_a_stranger_beside_our_stale_key_is_read_and_spared` red. Seven of eight
  mutations run were caught; the eighth is finding #6.
- **The verify task cannot pass vacuously.**
  `'RPM-KEY-ACTION none' not in chrome_key_verify.stdout_lines` is exact list
  membership, not a substring test on `stdout`; empty stdout fails; `rc != 0` is a
  separate disjunct.
- **gpg parsing verified against the real key, not fixtures.** Fetched
  `linux_signing_key.pub` live: one certificate, primary `7721F63BD38B4796`, 8
  subkeys, 5 marked `e`, signer `FD533C07C264648F` present with capability `s`.
  Field 2 = validity, 5 = keyid, 12 = capabilities all confirmed, and
  `len(fields) < 12` is the right bound. The file-path and `-` (stdin) forms of
  `gpg --show-keys --with-colons` produce identical validity/capability output, so
  the published-vs-installed seam is clean.
- **Fail fast.** No `failed_when: false` or `ignore_errors` anywhere in the scoped
  diff. Every `subprocess.run` passes `check=True`
  (`subkeys.py:163, 179, 203, 215`), and `FakeRunner.__call__` raises when
  `check is not True` — the rule is enforced by the fake, not assumed.
- **Remaining idempotence.** `rpm_key.is_key_imported` uppercases the id
  (`rpm_key.py:179-187`) and gpg prints field 5 uppercase, so there is no
  case-mismatch re-import loop; Decide/Verify are `changed_when: false`; an empty
  erase loop skips rather than reports changed.
- **`play-claude-code.yml`.** `rejectattr('stat.exists')` verified against this
  Jinja (3.1.6): dotted attributes resolve, and the expression returns 0 when all
  files exist. The stat/assert split is a strict improvement over `failed_when` on
  a loop.
- **Helper rules.** Stdlib only, no `__init__.py`, `unittest`, test path mirrors
  the helper path, 40 tests, marker lines are the only stdout.
- **Public repo.** No usernames, hostnames, home paths, emails or private IPs in
  the scoped diff, plan docs or journal. `**Owner**: joseph` matches 95+ existing
  plans and is the maintainer's public handle.
- **Plan script standards.** R1 bootstrap verbatim; R2 `plan_require_host`
  (journal records it firing); R4 `plan_start_log auto`; R7 `plan_mode gather` +
  `plan_gather_leg` per section; R9 no verdict; R10 report in `PLAN_RUN_DIR`; R12
  both scripts 0755. `shellcheck -x` clean on both.
- **Plan drift.** `CLAUDE/Plan/README.md:39` index row present; every ticked task
  matches what landed; 4.2 and 4.3 correctly still open; `docs/playbooks.md:326`
  already describes vendor DNF repositories, so no doc drift.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — 906 files, 1399 helper tests. The 15 semgrep
  partial-parse advisories are pre-existing; none is a Plan 00124 file.
- `hooks-daemon plan-qa --sweep`: **2 advise, 0 block** — plan 00046 stale path
  and journal-freshness across 12 old plans. Neither touches 00124.
- `ansible-playbook --syntax-check`: **OK** for all three changed playbooks.
- `scripts/qa-helper-tests.bash`: **triggered** (helpers/ and tests/helpers/
  changed) — ran inside qa-all and separately, 40/40 for
  `tests.helpers.rpm_keys.test_subkeys`.
- `check_extension_compat` / extension ESLint: **not triggered** — no
  `extensions/` metadata or JS change in scope.
