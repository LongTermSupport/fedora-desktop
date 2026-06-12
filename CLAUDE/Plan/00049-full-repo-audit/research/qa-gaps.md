# QA & Tooling Gaps Audit

> **Redaction note**: real identifiers quoted as evidence are replaced here with
> `<email-a>`-style placeholders, per the same public-repo rule this audit enforces
> (and to avoid re-leaking the SEC-01 data in a new tracked file). The cited
> file:line references locate the actual values in the repo.

## Scope & Method

This audit covers the QA tool-chain of the `fedora-desktop` repository: `scripts/qa-*.bash`, `.semgrep/`, `scripts/git-hooks/` (pre-commit and commit-msg), `scripts/lint`, the ESLint setup under `extensions/`, and — critically — what is *not* covered by any gate. Excluded per brief: `.git/`, `node_modules/`, `untracked/`, `.claude/hooks-daemon/` (upstream), `roles/vendor` (skimmed only).

Method:

1. Enumerated all 407 non-excluded tracked/working files and every shell, Python, JavaScript and YAML artefact.
2. Read every QA script in full: `scripts/qa-all.bash`, `qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`, `qa-ansible.bash`, `qa-ctrl-z-patch.bash`, `scripts/lint`, both git hooks, `.semgrep/bash-conventions.yml` and its test file.
3. **Executed** the real QA suite (`./scripts/qa-all.bash`, exit 0) inside the container and analysed the resulting `/tmp/qa-results.json` to establish ground truth on file discovery and severity handling.
4. Empirically tested the pre-commit CCY version-bump pipeline, the Ansible 2.19 parse hazards against both PyYAML and `ansible-playbook --syntax-check`, and ran `eslint .` and `node --check` on all JS.
5. Cross-checked documentation claims in `CLAUDE/QA.md`, `CLAUDE/ContainerRules.md` and `playbooks/CLAUDE.md` against actual behaviour.

All findings below cite files I read and commands I ran; severities follow the audit guide.

## Summary

The QA suite is well-structured (LLM-friendly JSON output, fail-fast exit-code-2 handling for missing tools, a dedicated behavioural test for the fragile ctrl+z patch) but has four serious holes:

1. The **CCY version-bump pre-commit enforcement is dead code** — a broken grep pipeline means it can never reject a commit (verified empirically).
2. **No YAML or Ansible syntax validation exists anywhere in the QA pipeline** — for a repository whose core product is Ansible. The two recorded Ansible 2.19 parse-hazard incidents (memory notes) would still ship undetected today; I demonstrated that the apostrophe-in-shell-comment hazard passes everything in `qa-all.bash` yet fails `ansible-playbook --syntax-check` (exit 4) in seconds. An `ansible-lint` wrapper (`scripts/lint`) exists but is integrated nowhere and documented nowhere.
3. **There is no CI at all** (no `.github/` directory), so every gate is local and bypassable — and the repository contains proof of bypass: a tracked file named U+00A0 leaking `/home/<user>` paths into a public repo, plus junk tracked files `localhost`/`loclahost`.
4. **shellcheck never gates** — 310 findings are advisory, including 3 *error*-level ones, two of which are genuine latent bugs in repo-owned deployed scripts.

Additional medium findings: out-of-scope upstream/vendor code dominates the bash scan (70 % of scanned files, 87 % of shellcheck noise), `qa-ansible.bash` only greps `playbooks/`, the Semgrep fail-fast ruleset contains exactly one rule, the QA scripts themselves swallow tool crashes with `|| true`, and two existing pytest suites are never executed.

---

## QA-01: CCY version-bump pre-commit check is dead code (broken grep pipeline)

**Evidence**: `scripts/git-hooks/pre-commit:72`:

```bash
if ! git diff --cached HEAD -- "$CCY_SCRIPT" | grep -q "^[+-]" | grep -v "^[+-]#" | grep -v "^[+-]$"; then
    # Only comments or whitespace changed - allow it
    echo "✓ CCY script: only comments/whitespace changed (version OK)"
else
    ...COMMIT REJECTED: CCY version bump required... exit 1
fi
```

`grep -q` produces **no output**, so the two subsequent `grep -v` stages always receive empty input and exit 1. The pipeline's exit status is that of the last command — always 1 — so `if !` is **always true**, and the "only comments/whitespace changed" branch is taken unconditionally. Verified empirically:

```
$ if ! printf "+realcode\n-removed\n" | grep -q "^[+-]" | grep -v "^[+-]#" | grep -v "^[+-]$"; then echo only-comments-OK; else echo REJECT; fi
only-comments-OK            # wrong — real code changed
```

**Impact**: `CLAUDE/ContainerRules.md` promises "Pre-commit hook will REJECT the commit" when `files/var/local/claude-yolo/claude-yolo` is modified without a `CCY_VERSION` bump, and the CCY runtime hash-validation depends on bumps being enforced. In reality any code change with an unchanged version commits cleanly, silently defeating the container-version drift detection (users would later see "DEVELOPER ERROR: CCY script modified without version bump" at runtime instead).

**Recommendation**: Replace the pipeline with a correct test, e.g. `if git diff --cached HEAD -- "$CCY_SCRIPT" | grep -E '^[+-]' | grep -vE '^[+-]{3}' | grep -vE '^[+-]\s*(#|$)' | grep -q .; then REJECT; fi`, and add a regression test (commit a code change without bump in a throwaway repo and assert the hook exits 1). Note the existing logic also fails to strip the `+++`/`---` diff headers.

---

## QA-02: No YAML/Ansible syntax validation anywhere in QA — `--syntax-check` never integrated

**Evidence**:

- `scripts/qa-all.bash:25-59` runs exactly four checks: `qa-bash.bash` (bash -n + advisory shellcheck), `qa-python.bash` (py_compile + ruff), `qa-patterns.bash` (Semgrep bash rules), `qa-ansible.bash` (a grep for unannotated `failed_when: false`/`ignore_errors:`). **None of them parses YAML at all** — not even `yaml.safe_load`.
- `rg -n 'syntax-check|ansible-lint|yamllint'` across the repo shows `ansible-playbook --syntax-check` only in docs and plan files as a *manual* step (`docs/development.md:166`, multiple `CLAUDE/Plan/*/PLAN.md`), never in any script invoked by `qa-all.bash`.
- The known 2.19 hazards (memory notes `project_ansible_219_quote_balance`, `project_ansible_219_task_name_colons`) were verified live: a playbook with an apostrophe in a `#` comment inside a `shell: |` block **passes PyYAML** (`PyYAML PARSED OK`) but fails `ansible-playbook --syntax-check` with exit 4: `[ERROR]: Error loading tasks: failed at splitting arguments, either an unbalanced jinja2 block or quotes`. Task-name colon variants fail both — but since QA runs neither, both hazard classes ship to the host undetected either way.
- A full `ansible-lint` wrapper already exists at `scripts/lint` (325 lines, JSON output, severity-aware pass/fail) but is referenced nowhere in `qa-all.bash` or `CLAUDE/QA.md`; `.ansible-lint` config exists at repo root and tunes a `production` profile. All required tools (`ansible-playbook`, `ansible-lint`, `yamllint`) are already installed in the CCY container (verified with `command -v`) and on the host via `run.bash:616-620`.

**Impact**: The repository's primary artefact class (≈190 playbook YAML files) has zero machine validation before commit. Both recorded 2.19 incidents reached deploy time on the host before being discovered, exactly as the memory notes describe. This is the single largest coverage gap relative to what the repo is.

**Recommendation**: Add a `qa-ansible-syntax.bash` step to `qa-all.bash` that runs `ansible-playbook --syntax-check` over `playbooks/playbook-main.yml` plus every standalone `playbooks/imports/**/*.yml` (it is parse-only — safe in the CCY container, does not violate the "never run playbooks in the container" rule). Optionally fold `scripts/lint` (ansible-lint) in as a second gate, and update `CLAUDE/QA.md` accordingly.

---

## QA-03: No CI pipeline at all — every gate is local and bypassable, with proof of bypass already in history

**Evidence**:

- No `.github/` directory exists (`ls /workspace/.github` → no such file); no `.gitlab-ci.yml`, Circle, Travis or Woodpecker config either.
- The only enforcement is local: `playbooks/imports/play-git-hooks-security.yml:36-41` sets `core.hooksPath = scripts/git-hooks` (imported by `playbook-main.yml:13`), and both hooks explicitly print their own bypass instructions (`scripts/git-hooks/pre-commit:94-95`, `commit-msg:107-108`: "To bypass this check … `git commit --no-verify`").
- Proof the local-only model has already failed in this public repo:
  - A tracked file whose **name is a single U+00A0 character** (git ls-files entry 409; `od -c` → `302 240`) contains 3 060 bytes of `ls -l` output of the user's `~/.local/bin`, leaking the real username `joseph` and `/home/<user>/...` paths ~30 times. Committed in `3008ef68` ("feat: add toggle mode, debug logging, and panel indicator UI"). Today's pre-commit `/home/<user>` pattern (`pre-commit:35`) would catch this content, so the commit predates the hook or bypassed it. (Overlaps the security audit dimension — the leak itself needs history purging.)
  - Two zero-byte junk tracked files at repo root: `localhost` and `loclahost` (typo), committed in `6552e1ce` — stray shell-redirection artefacts no gate noticed.
- `qa-all.bash` runs only when an agent/developer remembers to (the hooks-daemon nags agents but cannot cover human commits or other clones).

**Impact**: Secret/PII scanning, fail-fast pattern checks and all syntax QA can be skipped with one flag or an uninstalled hook, with no server-side backstop. For a public repository this converts every local lapse into a permanent public leak (demonstrated).

**Recommendation**: Add `.github/workflows/qa.yml` running on push/PR: `./scripts/qa-all.bash`, `ansible-playbook --syntax-check` (per QA-02), `eslint .` in `extensions/`, and a history-aware secret scanner (gitleaks). Separately: delete `localhost`, `loclahost` and the U+00A0 file, and purge the U+00A0 file from history per `CLAUDE/SecurityRules.md` (BFG/filter-repo).

---

## QA-04: shellcheck is advisory-only — error-level findings (including two real shipped bugs) never fail QA

**Evidence**: `scripts/qa-bash.bash:67-75` collects shellcheck JSON but only prints `⚠ shellcheck: N issues`; `ERRORS` is never incremented, so shellcheck can never cause a non-zero exit. Live run: `./scripts/qa-all.bash` exits **0** while reporting **310 shellcheck issues** (3 error, 143 warning, 155 info, 9 style). The 3 error-level findings include two genuine bugs in repo-owned, host-deployed code:

- `files/usr/local/bin/qp:473` — SC2168 `'local' is only valid in functions` (`local pid=$!` at non-function scope; under `bash` this errors at runtime on that path).
- `scripts/check-displaylink-status.sh:83` — SC2144 `-d doesn't work with globs` (`if [ -d /usr/src/evdi-* ]` breaks as soon as two evdi versions exist).

`CLAUDE/QA.md` ("What qa-all.bash Runs": "shellcheck + `bash -n`") presents shellcheck as a check, and lists no limitation about it being non-gating.

**Impact**: An entire static-analysis tier silently does nothing to the pass/fail result; demonstrably buggy code has been deployed to the host through a "passing" QA run.

**Recommendation**: Fail QA on shellcheck `error`-level findings at minimum (after scoping the scan per QA-05); fix the two cited bugs; document the warning/info tiers as advisory in `CLAUDE/QA.md`.

---

## QA-05: qa-bash scans upstream/vendor/runtime files — 70 % of scanned files and 87 % of shellcheck noise are out of scope

**Evidence**: `scripts/qa-bash.bash:25-47` excludes `.git`, `.ansible/roles`, `.claude/ccy/plugins`, `.claude/ccy/file-history`, `node_modules`, `untracked` — but **not** `.claude/hooks-daemon/` (the upstream dependency the audit brief and `qa-python.bash:33` both exclude), **not** `roles/vendor/`, **not** `.claude/ccy/shell-snapshots/` (runtime artefacts), **not** `.claude/skills/`. Ground truth from the live run (`/tmp/qa-results.json`): of 196 files scanned, 104 are `.claude/hooks-daemon`, 23 `roles/vendor`, 11 `.claude/hooks`, 6 `.claude/skills`, 1 `.claude/ccy/shell-snapshots/...` (which produced one of the three shellcheck *errors*). Of the 310 shellcheck issues, 230 are in `roles/vendor` and 41 in `.claude/` — repo-owned signal is 38 issues, buried. `qa-patterns.bash` is inconsistent the other way: Semgrep's default dot-directory ignore means `.claude/**` is silently never pattern-scanned (the 47 `|| echo` occurrences in hooks-daemon shell scripts are invisible), while `roles/vendor` *is* scanned. Also `qa-bash.bash:35` emits `warning: command substitution: ignored null byte` four times because `head -n1` is run on binary executables during shebang detection.

**Impact**: QA results are dominated by code the project must not modify, which (a) makes gating on shellcheck impossible (chicken-and-egg with QA-04), (b) wastes scan time, and (c) means a vendor update could break `bash -n` and block unrelated commits.

**Recommendation**: Align all three discovery lists: exclude `.claude/hooks-daemon`, `.claude/ccy` (whole tree), `.claude/skills` and `roles/vendor` in `qa-bash.bash`, and add `--exclude` flags for `roles/vendor` and `.claude` to `qa-patterns.bash` (making the implicit Semgrep dot-dir ignore explicit). Skip binary files before `head -n1` (e.g. `grep -Iq . "$file"`).

---

## QA-06: qa-ansible.bash fail-fast grep scope is too narrow

**Evidence**: `scripts/qa-ansible.bash:13` — `grep -rn --include='*.yml' -E "$patterns" playbooks/`. Gaps:

- Only `playbooks/` is scanned. `tasks/ensure-jq.yml` (imported by playbooks), `vars/`, `environment/localhost/`, and any future non-vendor `roles/` content are unscanned. (Currently zero violations exist in those paths — verified — so impact is latent, not active.)
- Only `*.yml`; a `*.yaml` file would escape (none exist today).
- The pattern `'failed_when: false|ignore_errors: true|ignore_errors: yes'` misses `ignore_errors: True` (YAML 1.1-valid capitalisation) and templated values such as `ignore_errors: "{{ var }}"`.
- The `# FAIL-FAST-OK:` annotation must be on the same line as the directive (line-by-line `grep -q 'FAIL-FAST-OK'` at line 20) — consistent with current usage (all 59 annotations pass) but undocumented.
- Results are not merged into `/tmp/qa-results.json` (`qa-all.bash:54-59`, comment "no JSON output"), so machine consumers of the QA JSON never see ansible failures; it also uses a fixed temp path `/tmp/qa-ansible-matches` (line 13-16).

**Impact**: A fail-fast violation placed in `tasks/` or spelt `True` would pass the project's #1-rule gate.

**Recommendation**: Widen the grep to `playbooks/ tasks/ vars/ environment/ roles/ --include='*.yml' --include='*.yaml'`, make the pattern case-insensitive for boolean values, emit the same JSON shape as the other checks, and use `mktemp`.

---

## QA-07: Semgrep fail-fast ruleset has exactly one rule — most prohibited error-hiding patterns unchecked

**Evidence**: `.semgrep/bash-conventions.yml` contains a single rule, `bash-error-hiding-pipe-echo` (`$CMD || echo $MSG`). `CLAUDE.md`/the hooks-daemon `error_hiding_blocker` prohibit a much broader family: `|| true`, `|| :`, stderr-to-`/dev/null` failure silencing, empty error handling. Repo-owned bash currently contains 55 `|| true` occurrences (scripts/, files/, run.bash, vault.bash, fedora-install/) that no repo-side QA examines — some are legitimate arithmetic guards (`((ERRORS++)) || true`), others swallow real tool failures (see QA-08). The daemon blocks *new edits* in agent sessions only; manual edits, host-side edits and pre-existing code have no check. `.semgrep/tests/bash-conventions.bash` exists (good practice) but covers only the one rule, and nothing in QA runs `semgrep --test` against it.

**Impact**: The project's #1 rule is enforced repo-wide for only one syntactic shape; the dominant suppression idiom (`|| true`) is invisible to `qa-all.bash`.

**Recommendation**: Add rules for `$CMD || true` and `$CMD || :` with `pattern-not` carve-outs for arithmetic/counter idioms and a `# FAIL-FAST-OK:` same-line annotation escape, mirroring the Ansible convention. Add a `semgrep --test .semgrep/` step to `qa-patterns.bash` so the rule test file is actually exercised.

---

## QA-08: QA scripts themselves swallow tool crashes (fail-fast violations inside the gate)

**Evidence**:

- `scripts/qa-python.bash:75-77`:
  ```bash
  ruff check --fix "${PY_FILES[@]}" >/dev/null 2>&1 || true
  ruff_raw=$(ruff check --output-format json "${PY_FILES[@]}" 2>/dev/null) || true
  RUFF_JSON="${ruff_raw:-[]}"
  ```
  If ruff *crashes* (exit ≥ 2 — bad config, internal error), stderr is discarded, `ruff_raw` is empty, `RUFF_JSON` becomes `[]` and the run is reported as **pass**. A linter crash is indistinguishable from a clean lint. Additionally, `ruff check --fix` silently **mutates the working tree during a check run** with no notice that files changed.
- `scripts/qa-bash.bash:68-70`: `xargs -0 shellcheck --format json 2>/dev/null | jq -s 'add // []' > "$TMP_SC" || true` — a shellcheck or jq crash likewise degrades to "0 issues".

Both contradict the repo's own probe-then-fail doctrine (an exit-code-2 "missing tool" path exists, but a *crashing* tool path does not).

**Impact**: The QA gate can green-light a commit while one of its analysers silently failed — the precise failure mode the project's fail-fast rule exists to prevent.

**Recommendation**: Capture the exit codes explicitly (ruff: treat rc 0/1 as data, rc ≥ 2 as `exit 2`; shellcheck: same pattern), keep stderr in a temp file for the error message, and either drop `--fix` from the check path or report mutated files in the summary.

---

## QA-09: Existing pytest suites are never executed by any QA gate

**Evidence**: `tests/clip_scan/test_clip_scan.py` (+ `conftest.py`, with populated `__pycache__` proving local runs) and `scripts/test_config_merge.py` exist, but no script under `scripts/` and nothing in `CLAUDE/QA.md` or `qa-all.bash` invokes pytest (`rg pytest` matches only container docs). `qa-python.bash` merely byte-compiles and ruff-checks the test files.

**Impact**: Regressions in `scripts/config_merge.py` or the clip-scan tool pass QA; the tests rot silently (e.g. the bash test harnesses `scripts/test-ccy-ssh-probe.bash` and `.semgrep/tests/bash-conventions.bash` are likewise manual-only).

**Recommendation**: Add a `qa-pytest.bash` step (exit 2 if pytest missing, per the missing-dependency rule — pytest installation belongs in a playbook/container image if absent) and wire it into `qa-all.bash`; document it in `CLAUDE/QA.md`.

---

## QA-10: JavaScript QA is manual, partial, and ccy-ctrl-z-patch.js has no lint coverage

**Evidence**: ESLint is not part of `qa-all.bash`; `CLAUDE/QA.md` ("GNOME Shell Extension JavaScript") prescribes a manual `eslint speech-to-text@fedora-desktop/extension.js` — one file, though four JS files exist under `extensions/` (`eslint .` works and currently passes, exit 0 — verified). `files/var/local/claude-yolo/ccy-ctrl-z-patch.js` is outside the `extensions/` ESLint project entirely; its only QA is the (good) behavioural `scripts/qa-ctrl-z-patch.bash`, which is also manual and needs an `npm install` on first run. `extensions/package.json` pins `eslint ^8.57.1`, which is end-of-life (no security fixes).

**Impact**: A syntax error in `ccy-ctrl-z-patch.js` or a non-speech-to-text extension can be committed through a green `qa-all.bash`; lint discipline relies on the agent remembering a side command.

**Recommendation**: Add a `qa-js.bash` step that runs `node --check` over all repo JS plus `extensions/node_modules/.bin/eslint .` when `node_modules` is present (exit 2 otherwise); update `CLAUDE/QA.md` to lint all extensions; plan an ESLint 9 migration.

---

## QA-11: CLAUDE/QA.md misdescribes the suite

**Evidence**:

- `CLAUDE/QA.md:61` ("When to Run What"): "Ansible playbooks | `./scripts/qa-all.bash` (includes `qa-ansible.bash` via `qa-patterns.bash`)" — false: `qa-ansible.bash` is invoked directly by `qa-all.bash:56`; `qa-patterns.bash` runs Semgrep only.
- The "What qa-all.bash Runs" table omits `qa-ansible.bash` entirely.
- Nothing documents that shellcheck is advisory (QA-04) or that ruff auto-fixes files during the run (QA-08), and `scripts/lint` is documented nowhere.

**Impact**: Agents reading the canonical QA doc draw wrong conclusions about coverage (e.g. believing pattern QA covers Ansible).

**Recommendation**: Correct the table and the "When to Run What" row, add a row for `qa-ansible.bash`, and document advisory behaviours and `scripts/lint`.

---

## QA-12: qa-bash output polish bugs

**Evidence**:

- `scripts/qa-bash.bash:72` prints `⚠ shellcheck: N issues (see $JSON_OUT .shellcheck_diagnostics)`. When run via `qa-all.bash`, `$JSON_OUT` is the parent's `mktemp` file (`qa-all.bash:18,28`), which the parent's `trap … EXIT` deletes — the live run printed `see /tmp/tmp.VOR3Az4BXF`, a path that no longer exists. The diagnostics actually survive in `/tmp/qa-results.json` under `.checks.bash.shellcheck_diagnostics`.
- `qa-bash.bash:35` floods stderr with `warning: command substitution: ignored null byte in input` (×4) when shebang-sniffing binary executables.

**Impact**: Users are pointed at deleted files; warning noise erodes trust in QA output.

**Recommendation**: Print the merged-JSON location when `QA_JSON_OUT` is set (or have `qa-all.bash` print the canonical pointer), and screen out binaries before `head -n1`.

---

## QA-13: Secret-scanning hooks have pattern and logic blind spots

**Evidence** (`scripts/git-hooks/pre-commit:16-40`, `commit-msg:31-49`):

- No detection of **private key blocks** (`-----BEGIN … PRIVATE KEY-----`) — only `ssh-rsa`/`ssh-ed25519` *public* keys.
- No patterns for `github_pat_` (fine-grained PATs), `glpat-`, `ghs_`/`ghr_`, OpenAI `sk-` (only `sk-ant-`), Slack `xox`, AWS *secret* keys (only `AKIA` access-key IDs), or generic high-entropy strings.
- Whole-file whitelist logic: `pre-commit:139-143` — if *any* line matching the `/home/` pattern also matches `files/home/(bashrc-includes|[a-z-]+)`, the **entire file** is skipped for that pattern, so one legitimate `files/home/...` reference whitelists a real `/home/<user>/...` leak elsewhere in the same file. Same construct at `commit-msg:66-69`.
- Scanning is pattern-list only; no gitleaks/detect-secrets layer, and (per QA-03) no server-side re-scan.

**Impact**: Realistic credential shapes and a crafted/accidental mixed file pass the public-repo gate; combined with `--no-verify` bypassability the protection is best-effort only.

**Recommendation**: Add the missing token patterns and a private-key-block pattern; change the whitelist logic to filter matching *lines* (not skip the file) before deciding; adopt gitleaks locally and in CI as defence in depth.

---

## QA-14: File classes with zero QA, and an acknowledged-but-unimplemented playbook check

**Evidence / inventory of uncovered types**:

- **Dockerfiles** (`files/var/local/claude-yolo/Dockerfile`, `Dockerfile.example-ansible`, `.devcontainer` images): no hadolint or equivalent.
- **Markdown**: no link checker or spell/style check for `docs/` despite British-English policy (daemon auto-formats tables on edit, but nothing validates committed content or links).
- **YAML style**: `yamllint` is installed in the CCY image (`.claude/ccy/Dockerfile:32`) and on the host but unused (subsumed by QA-02's ansible-lint recommendation).
- **Playbook executability**: `playbooks/CLAUDE.md` ("Pre-commit Hook Recommendation") explicitly proposes a pre-commit check that every `playbooks/**/*.yml` has the `#!/usr/bin/env ansible-playbook` shebang and exec bit; only the fixer (`scripts/make-playbooks-executable.bash`) exists — no check anywhere.
- **JSON** (`extensions/*/metadata.json`, `.eslintrc.json`, etc.): no `jq empty` validation pass.

**Impact**: Low individually; collectively these are easy wins that prevent doc rot and broken playbook ergonomics.

**Recommendation**: Fold a shebang/exec-bit assertion into `qa-ansible.bash` (cheap, satisfies the documented recommendation); consider lychee/markdown-link-check and hadolint in the future CI from QA-03.

---

## Positive Observations

- **Missing-tool fail-fast is exemplary**: `qa-all.bash`, `qa-python.bash` and `qa-patterns.bash` use a dedicated exit code 2 ("missing required tool — refuse to run entirely") instead of skip-if-absent, exactly per the project's missing-dependency rule.
- **LLM-friendly design**: every QA script emits terse stdout plus structured JSON with documented jq recipes; `qa-all.bash` merges results into one machine-readable artefact.
- **`qa-ctrl-z-patch.bash` is a model behavioural test**: it tests the real patch against the latest published Claude Code on a temp copy, distinguishes known-pattern vs dynamic application, and never touches the cached install.
- **Semgrep rules ship with a test corpus** (`.semgrep/tests/bash-conventions.bash` with `ruleid:`/`ok:` annotations) — rare discipline for a one-rule config.
- **Hook installation is itself IaC**: `play-git-hooks-security.yml` asserts hook existence/executability and sets `core.hooksPath` idempotently from the main playbook, and `core.hooksPath` is correctly active in the CCY container clone.
- **The fail-fast annotation convention works in practice**: all 59 `failed_when/ignore_errors` instances in `playbooks/` carry same-line `# FAIL-FAST-OK:` justifications and `qa-ansible.bash` correctly verifies them.
- **ESLint config encodes hard-won domain knowledge**: `extensions/.eslintrc.json` bans GNOME-Shell-freezing synchronous subprocess calls via `no-restricted-syntax` selectors — and `eslint .` currently passes across all four JS files.

---

## Adversarial Verification Appendix

### QA-01 — CONFIRMED (high confidence) (severity adjusted to **medium**)

CONFIRMED. /workspace/scripts/git-hooks/pre-commit:72 pipes `grep -q "^[+-]"` into two `grep -v` stages; grep -q emits no output, so the final grep -v always sees empty input and exits 1, and the pipeline status (last command) is always 1. With `!` negation the 'only comments/whitespace changed' branch is taken unconditionally whenever CCY_VERSION is unchanged, making the COMMIT REJECTED path (lines 76-97) unreachable dead code. Empirically reproduced: both a real code-change diff and a comment-only diff produce pipeline exit 1. This contradicts CLAUDE/ContainerRules.md's promise that the pre-commit hook rejects unbumped CCY changes. Severity adjusted high->medium: a compensating runtime control exists — the deployed claude-yolo script (files/var/local/claude-yolo/claude-yolo:303-331) validates CCY_HASH vs CCY_VERSION at run time, prints 'DEVELOPER ERROR: CCY script modified without version bump', and forces reconfiguration, so unbumped changes are detected and self-healed at user run time rather than commit time. Impact is process-gate failure/doc drift, not security or data loss. Recommendation (filter +/- lines, strip +++/--- headers and comment/blank lines, then grep -q .) is correct; add a regression test that an unbumped code change is rejected.

### QA-02 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed by reading the files. qa-all.bash (lines 25-59) runs only qa-bash.bash, qa-python.bash, qa-patterns.bash, and qa-ansible.bash — none parse YAML. qa-ansible.bash is purely a grep for failed_when/ignore_errors patterns (line 13). scripts/lint is a complete ansible-lint wrapper but is invoked by nothing: repo-wide grep over scripts/ and .semgrep/ finds zero occurrences of 'syntax-check' or 'yamllint', and 'ansible-lint' only inside scripts/lint itself. Git hooks (pre-commit, commit-msg) contain no qa/lint/syntax/yaml logic — secret scanning only — so there is no compensating control in the commit path. The auditor's live-test claim is corroborated by project memory (project_ansible_219_task_name_colons.md: 'PyYAML/qa-all.bash do not catch this — use ansible-playbook --syntax-check'), i.e. both recorded Ansible 2.19 incident classes provably escape QA. CLAUDE/QA.md also overstates coverage by directing 'Ansible playbooks → qa-all.bash' (and incorrectly says qa-ansible runs 'via qa-patterns.bash' — qa-all.bash calls it directly). Severity adjusted high→medium: the gap is real and has recurred, but a broken playbook fails loudly at parse time at deploy (fail-fast preserved, just deferred), with no security impact and an easy fix (wire ansible-playbook --syntax-check and/or scripts/lint into qa-all.bash, update CLAUDE/QA.md).

### QA-03 — CONFIRMED (high confidence)

Every claim confirmed against the repo. (1) No CI: /workspace/.github does not exist and no workflow YAML exists anywhere in the repo — enforcement is solely local git hooks installed via core.hooksPath (playbooks/imports/play-git-hooks-security.yml:38,45). (2) Bypassable with printed instructions: scripts/git-hooks/pre-commit lines 95 and 184 and commit-msg line 108 literally echo 'git commit --no-verify' as the override (auditor's cited lines 94/107 are off by one — immaterial). (3) Leak file: a tracked file literally named U+00A0 ("\302\240", added in commit 3008ef68, still present in HEAD per git ls-files and on disk) contains ls -l output of ~/.local/bin leaking the real username 'joseph' and ~12 absolute /home/<user>/... paths — a direct violation of the public-repo rule (SecurityRules.md: no usernames, no /home paths) that the hooks failed to catch. (4) Junk files: empty tracked files 'localhost' and 'loclahost' (0 bytes each) added in commit 6552e1ce, still tracked. Note the leak is compounded by commit author metadata (joseph <<email-a>>) which makes the username public anyway, but the tracked file still violates the project's own hard rule and proves the local-only gates miss accidental adds (the hooks scan content patterns, not odd filenames/empty files). Severity high is appropriate: confirmed personal-info leak in a tracked file of a public repo plus total absence of CI safety net. Recommendation in the finding is sound; history purge per SecurityRules.md would also be needed since deletion alone leaves the file in history.

### QA-04 — CONFIRMED (high confidence)

Confirmed by reading scripts/qa-bash.bash and executing it. (1) Gating claim exact: ERRORS increments only on bash -n failures; shellcheck block (lines 67-75) uses '|| true' and merely echoes a warning count; script exited 0 while reporting exactly 310 shellcheck issues across 196 files. (2) Level breakdown verified from JSON output: 3 error, 143 warning, 155 info, 9 style. (3) qp:473 verified: 'local pid=$!' is top-level code (last function closes at line 455; the 'if $open_web' block at 458 is column-0 main script) — genuine runtime bug in a deployed script (bash errors 'local: can only be used in a function', pid unset, bad PID_FILE write). (4) check-displaylink-status.sh:83 verified: '[ -d /usr/src/evdi-* ]' fails with multiple glob matches (SC2144). (5) CLAUDE/QA.md table lists qa-bash.bash as 'shellcheck + bash -n', presenting shellcheck as an active check. Minor correction: the third error-level finding is in .claude/ccy/shell-snapshots/ (ephemeral artifact, a scan-scope issue per QA-05), so repo-owned error-level bugs number 2 — exactly as the auditor stated. No compensating control found. Severity high stands given fail-fast is the project's #1 hard rule and the primary QA gate silently passes with error-level findings in deployed scripts.


