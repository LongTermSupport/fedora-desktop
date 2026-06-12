# Triage — Full Repository Audit (Plan 00049)

**Source**: 10-dimension multi-agent audit, 2026-06-12 (workflow `wf_70cb99e0-10f`; 25 agents).
**Findings**: 134 total — 7 high, 64 medium, 50 low, 13 info after adversarial verification.
**Research docs**: one per dimension under [research/](research/); each finding ID links to its detailed evidence section. Verification verdicts are appended to each research doc.
**Action plan**: the selected must-do work is in [PLAN.md](PLAN.md).

## How to read this

- **Severity** is the *effective* severity: where an adversarial verifier confirmed a finding but adjusted its severity, the adjusted value is used (noted in the Verified column).
- **Verified** — ✅ means an independent agent re-read the cited files and confirmed the claim (only critical/high findings were verified; medium and below are single-source).
- **Related** — the same root cause was independently found by more than one auditor; fix once, close all.

## Top themes

1. **Committed PII in a public repo** ([SEC-01](research/security.md#sec-01-real-pii-and-account-mappings-committed-in-tracked-plan-documents)) — real emails, account mappings and hostnames in tracked plan docs, plus junk tracked files including a U+00A0-named file leaking a home-directory listing ([OPP-01](research/opportunities.md#opp-01-stray-tracked-artefacts-at-repo-root-localhost-loclahost-and-a-u00a0-named-file-leaking-a-home-directory-listing)). The committed secret scanner would not have caught any of it ([SEC-02](research/security.md#sec-02-committed-pre-commit-secret-scanner-has-coverage-gaps-that-miss-the-sec-01-leak-class), [QA-13](research/qa-gaps.md#qa-13-secret-scanning-hooks-have-pattern-and-logic-blind-spots)).
2. **The QA gate does not gate** — the CCY version-bump pre-commit check is dead code ([QA-01](research/qa-gaps.md#qa-01-ccy-version-bump-pre-commit-check-is-dead-code-broken-grep-pipeline)), shellcheck never fails QA and is skipped when absent ([QA-04](research/qa-gaps.md#qa-04-shellcheck-is-advisory-only-error-level-findings-including-two-real-shipped-bugs-never-fail-qa)), analyser crashes report as a pass ([QA-08](research/qa-gaps.md#qa-08-qa-scripts-themselves-swallow-tool-crashes-fail-fast-violations-inside-the-gate)), there is no Ansible syntax-check despite two recorded 2.19 incident classes ([QA-02](research/qa-gaps.md#qa-02-no-yamlansible-syntax-validation-anywhere-in-qa---syntax-check-never-integrated)), no pytest, no ESLint stage, and no CI at all ([QA-03](research/qa-gaps.md#qa-03-no-ci-pipeline-at-all-every-gate-is-local-and-bypassable-with-proof-of-bypass-already-in-history)).
3. **Fail-fast rule #1 violated inside playbooks** — most multi-line `shell: |` blocks lack `set -e`, so intermediate failures pass silently ([FF-01](research/fail-fast.md#ff-01-multi-line-shell-blocks-without-set--e-intermediate-failures-silently-swallowed), [ANS-01](research/ansible.md#ans-01-multi-command-shell-blocks-without-set--e-mask-intermediate-failures)); cloudflare-warp can permanently wedge itself broken ([ANS-02](research/ansible.md#ans-02-cloudflare-warp-repo-install-can-write-an-empty-repo-file-then-creates-permanently-skips-repair)).
4. **CCY isolation weaker than advertised** — the entire `$XDG_RUNTIME_DIR` is mounted read-write into a YOLO container ([SEC-03](research/security.md#sec-03-ccy-container-bind-mounts-the-entire-host-xdg_runtime_dir-read-write)), tokens are visible in `/proc` cmdline ([BSH-09](research/bash.md#bsh-09-oauth-and-github-tokens-passed-as--e-varvalue-visible-in-proc-cmdline)), and several token/SSH flows have real bugs ([BSH-04](research/bash.md#bsh-04-create_token-tee-masks-container-exit-code-failure-diagnostics-are-unreachable)–[BSH-06](research/bash.md#bsh-06-multi-key-ssh-flow-github_username-from-last-key-token-alias-from-first-key), [BSH-14](research/bash.md#bsh-14-ccy---connect-uses-a-different-project-name-derivation-than-container-creation)).
5. **Documentation actively misleads** — Docker described as rootless/optional, vault edit instructions broken, a doc for a playbook that no longer exists, and CLAUDE/PlanWorkflow.md describing a different project entirely ([DOC-01](research/docs.md#doc-01-docker-documented-as-rootlessoptional-it-is-rootful-and-core)–[DOC-04](research/docs.md#doc-04-claudeplanworkflowmd-describes-a-different-projects-qa-and-planning-infrastructure)).
6. **Shipped runtime bugs** — `qp` web-launch path dies on every cold start ([BSH-07](research/bash.md#bsh-07-qp-local-outside-a-function-aborts-the-web-launch-path-at-runtime)), `setup.bash` crashes on version mismatch ([BSH-08](research/bash.md#bsh-08-setupbash-calls-undefined-warn-version-mismatch-path-crashes-instead-of-prompting)), `ccy --connect` cannot find containers under non-generic parent dirs ([BSH-14](research/bash.md#bsh-14-ccy---connect-uses-a-different-project-name-derivation-than-container-creation)), and the speech-to-text extension interpolates an unvalidated setting into `bash -c` ([EXT-02](research/extensions.md#ext-02-language-setting-bypasses-the-extensions-own-shell-argument-sanitisation-inside-bash--c)).


## High — must address (7)

Real rule violations or bugs with concrete impact, all independently confirmed by adversarial verification.

| ID | Title | Area | Effort | Verified | Related |
|----|-------|------|--------|----------|---------|
| [SEC-01](research/security.md#sec-01-real-pii-and-account-mappings-committed-in-tracked-plan-documents) | Real PII and account mappings committed in tracked plan documents | security | medium | ✅ confirmed (high) |  |
| [FF-01](research/fail-fast.md#ff-01-multi-line-shell-blocks-without-set--e-intermediate-failures-silently-swallowed) | Multi-line shell: \| blocks without set -e — intermediate failures silently swallowed | fail-fast | medium | ✅ confirmed (high) | ANS-01 |
| [ANS-01](research/ansible.md#ans-01-multi-command-shell-blocks-without-set--e-mask-intermediate-failures) | Multi-command shell blocks without set -e mask intermediate failures | ansible | small | ✅ confirmed (high) | FF-01 |
| [ANS-02](research/ansible.md#ans-02-cloudflare-warp-repo-install-can-write-an-empty-repo-file-then-creates-permanently-skips-repair) | cloudflare-warp repo install writes empty repo file on curl failure, then creates: guard permanently skips repair | ansible | small | ✅ confirmed (high) |  |
| [BSH-02](research/bash.md#bsh-02-shellcheck-never-gates-qa-missing-shellcheck-is-silently-tolerated) | shellcheck never gates QA and is silently skipped when absent | bash | medium | ✅ confirmed (high) | FF-03, QA-04 |
| [QA-03](research/qa-gaps.md#qa-03-no-ci-pipeline-at-all-every-gate-is-local-and-bypassable-with-proof-of-bypass-already-in-history) | No CI pipeline; all gates local/bypassable, with committed evidence of bypass (U+00A0 file leaking /home/<user>, junk tracked files) | qa-gaps | medium | ✅ confirmed (high) | OPP-01 |
| [QA-04](research/qa-gaps.md#qa-04-shellcheck-is-advisory-only-error-level-findings-including-two-real-shipped-bugs-never-fail-qa) | shellcheck never gates — 310 issues advisory, including error-level real bugs in deployed scripts | qa-gaps | medium | ✅ confirmed (high) | BSH-02, FF-03 |

## Medium — should address (64)

Confirmed or single-source findings with limited but real impact. Several originally-high findings were adjusted down to medium during verification.

| ID | Title | Area | Effort | Verified | Related |
|----|-------|------|--------|----------|---------|
| [SEC-02](research/security.md#sec-02-committed-pre-commit-secret-scanner-has-coverage-gaps-that-miss-the-sec-01-leak-class) | Committed pre-commit secret scanner has coverage gaps that miss the SEC-01 leak class | security | medium | — | QA-13 |
| [SEC-03](research/security.md#sec-03-ccy-container-bind-mounts-the-entire-host-xdg_runtime_dir-read-write) | CCY container bind-mounts the entire host XDG_RUNTIME_DIR read-write | security | small | — | CCY-02 |
| [FF-02](research/fail-fast.md#ff-02-play-toolbox-installyml-uses-the-prohibited-skip-and-warn-pattern) | play-toolbox-install.yml uses prohibited skip-and-warn pattern | fail-fast | small | — | ANS-10 |
| [FF-03](research/fail-fast.md#ff-03-qa-bashbash-shellcheck-silently-optional-advisory-only-and-crash-masked) | qa-bash.bash: shellcheck silently skipped if absent, advisory-only, and crash-masked | fail-fast | small | — | BSH-02, QA-04 |
| [FF-04](research/fail-fast.md#ff-04-qa-pythonbash-ruff-crash-indistinguishable-from-no-findings) | qa-python.bash: ruff crash indistinguishable from no findings | fail-fast | small | — | BSH-13, QA-08 |
| [FF-05](research/fail-fast.md#ff-05-docker-in-lxc-warn-and-continue-verification-and-suppressed-npm-update) | docker-in-lxc: warn-and-continue verification and suppressed npm update | fail-fast | small | — |  |
| [ANS-03](research/ansible.md#ans-03-resolvedconf-deployed-but-the-reload-handler-is-commented-out-dns-config-silently-never-applies) | resolved.conf deployed but reload handler commented out — DNS config silently never applies | ansible | small | — |  |
| [ANS-04](research/ansible.md#ans-04-warp-registration-is-deleted-and-re-created-on-every-run) | Warp registration deleted and re-created on every playbook run | ansible | small | — |  |
| [ANS-05](research/ansible.md#ans-05-play-unifi-controller-hardcodes-podman-violating-the-container_engine-rule) | play-unifi-controller hardcodes podman instead of using the container_engine variable | ansible | small | — | OPP-12 |
| [ANS-06](research/ansible.md#ans-06-play-unifi-controller-error-hiding-in-deployed-launcher-shell-firewalling-and-meaningless-changed_when) | play-unifi-controller: '\|\| true' error-hiding in deployed launcher, shell firewalling, always-true changed_when | ansible | small | — |  |
| [ANS-07](research/ansible.md#ans-07-flatpak-become-misuse-and-inconsistency-between-play-comms-and-play-videography) | Flatpak become misuse and drift between play-comms and play-videography | ansible | small | — |  |
| [ANS-08](research/ansible.md#ans-08-global-git-ignore-blockinfile-fails-on-fresh-systems-parent-directory-never-created) | Global git ignore blockinfile fails on fresh systems — ~/.config/git never created | ansible | small | — |  |
| [ANS-09](research/ansible.md#ans-09-lxc-service-restarted-on-every-playbook-main-run) | LXC service restarted unconditionally on every main-playbook run | ansible | small | — |  |
| [ANS-10](research/ansible.md#ans-10-jetbrains-toolbox-post-install-launch-uses-skip-and-warn-instead-of-failing) | JetBrains Toolbox post-install launch uses prohibited skip-and-warn pattern | ansible | small | — | FF-02 |
| [ANS-11](research/ansible.md#ans-11-localhostymldist-missing-required-github_ssh_passphrase-documents-wrong-variable-name-lastfm_secret) | localhost.yml.dist missing required github_ssh_passphrase and documents wrong lastfm variable name | ansible | small | — |  |
| [ANS-12](research/ansible.md#ans-12-qobuz-playbook-secret-handling-shell-quoting-injection-hazard-world-readable-secrets-file-append-duplication-risk) | Qobuz playbook secret handling: shell-quoting injection hazard, world-readable secrets file, append duplication | ansible | medium | — |  |
| [BSH-01](research/bash.md#bsh-01-pre-commit-ccy-version-bump-enforcement-is-dead-code-pipeline-bug) | Pre-commit CCY version-bump enforcement is dead code (grep -q pipeline bug) | bash | small | ✅ confirmed (high), severity adjusted high→medium | QA-01 |
| [BSH-03](research/bash.md#bsh-03-qa-ansiblebash-fixed-temp-file-grep-failure-indistinguishable-from-clean-and-scans-only-playbooks) | qa-ansible.bash: fixed /tmp file, grep failure indistinguishable from clean, scans only playbooks/ | bash | small | — |  |
| [BSH-04](research/bash.md#bsh-04-create_token-tee-masks-container-exit-code-failure-diagnostics-are-unreachable) | create_token: `\| tee` without pipefail masks container exit code; failure diagnostics unreachable | bash | small | — |  |
| [BSH-05](research/bash.md#bsh-05-select_token-createrenew-paths-leave-selected_token-empty-set--e-crash-renewal-flows-end-in-spurious-cancelled) | select_token create/renew paths leave SELECTED_TOKEN empty, crashing ccy after successful token creation | bash | small | — |  |
| [BSH-06](research/bash.md#bsh-06-multi-key-ssh-flow-github_username-from-last-key-token-alias-from-first-key) | Multi-SSH-key flow derives GITHUB_USERNAME from the last key but the gh token from the first | bash | small | — |  |
| [BSH-07](research/bash.md#bsh-07-qp-local-outside-a-function-aborts-the-web-launch-path-at-runtime) | qp: `local pid=$!` outside a function aborts the web-launch path at runtime | bash | small | — |  |
| [BSH-08](research/bash.md#bsh-08-setupbash-calls-undefined-warn-version-mismatch-path-crashes-instead-of-prompting) | setup.bash calls undefined `warn` — Fedora version-mismatch path crashes instead of prompting | bash | small | — |  |
| [BSH-09](research/bash.md#bsh-09-oauth-and-github-tokens-passed-as--e-varvalue-visible-in-proc-cmdline) | OAuth and GitHub tokens passed as -e VAR=value, exposing secrets in /proc cmdline | bash | small | — |  |
| [BSH-10](research/bash.md#bsh-10-predictable-tmp-paths-without-mktemp-toctou-collision) | Predictable /tmp paths without mktemp (TOCTOU/collision), including gitconfig staging dir | bash | small | — |  |
| [BSH-11](research/bash.md#bsh-11-scriptslint-fix-mode-pipeline-masks-ansible-lint-exit-codes) | scripts/lint fix mode: grep pipeline masks ansible-lint exit codes and misreports clean runs | bash | small | — |  |
| [BSH-12](research/bash.md#bsh-12-desktop-symlinks-advertises-debug-mounts-that-are-never-applied) | desktop-symlinks advertises read-only debug mounts that are never applied (CCY_EXTRA_MOUNTS unread) | bash | small | — |  |
| [BSH-13](research/bash.md#bsh-13-qa-pythonbash-ruff-crash-is-indistinguishable-from-a-clean-pass) | qa-python.bash: ruff crash (rc>=2) reports a clean pass; --fix mutates files silently during QA | bash | small | — | FF-04, QA-08 |
| [BSH-14](research/bash.md#bsh-14-ccy---connect-uses-a-different-project-name-derivation-than-container-creation) | ccy --connect derives the project name differently from container creation, so it cannot find containers | bash | small | — |  |
| [CCY-01](research/ccy.md#ccy-01-heredoc-defect-corrupts-the-ai-guided-custom-dockerfile-prompt) | Heredoc defect corrupts AI-guided custom-Dockerfile prompt | ccy | small | — |  |
| [CCY-02](research/ccy.md#ccy-02-gui-passthrough-mounts-the-entire-xdg_runtime_dir-into-the-yolo-container) | GUI passthrough mounts entire XDG_RUNTIME_DIR into the YOLO container | ccy | small | — | SEC-03 |
| [CCY-03](research/ccy.md#ccy-03-ctrlz-patch-qa-only-exercises-the-legacy-clijs-path) | ctrl+z patch QA only validates the legacy cli.js path | ccy | medium | — |  |
| [EXT-01](research/extensions.md#ext-01-eslint-is-mandated-by-docs-but-not-wired-into-any-qa-gate) | ESLint mandated by docs but not enforced by qa-all.bash or pre-commit | extensions | small | — | QA-10 |
| [EXT-02](research/extensions.md#ext-02-language-setting-bypasses-the-extensions-own-shell-argument-sanitisation-inside-bash--c) | Unvalidated language setting interpolated into bash -c command string | extensions | small | — |  |
| [EXT-03](research/extensions.md#ext-03-startstop-race-second-insert-press-during-start-up-spawns-a-second-recorder-that-clobbers-the-pid-file) | Start/stop race: double Insert press spawns a second recorder that clobbers the PID file | extensions | small | — |  |
| [EXT-04](research/extensions.md#ext-04-silent-empty-catch-blocks-contravene-the-project-fail-fast-rule) | Silent empty catch blocks contravene the project fail-fast rule | extensions | small | — |  |
| [PERF-01](research/performance.md#perf-01-qa-allbash-spends-30-s-scanning-upstream-and-vendored-code) | qa-all.bash scans upstream hooks-daemon and vendored roles — ~30s wasted per pre-commit run | performance | small | — | QA-05 |
| [PERF-02](research/performance.md#perf-02-ccy-dockerfile-layer-cache-is-busted-by-any-edit-hash-label-at-top) | CCY Dockerfile: DOCKERFILE_HASH LABEL near the top busts the entire layer cache on any edit | performance | small | — |  |
| [PERF-03](research/performance.md#perf-03-play-rpm-fusionyml-runs-seven-unguarded-dnf-transactions-on-every-main-run) | play-rpm-fusion.yml runs seven unguarded dnf transactions on every playbook-main run | performance | small | — |  |
| [PERF-04](research/performance.md#perf-04-fwupd-firmware-refresh-forced-on-every-main-run) | fwupdmgr refresh --force re-downloads firmware metadata on every main run | performance | small | — |  |
| [PERF-05](research/performance.md#perf-05-recursive-chown-of-the-entire-nvm-tree-on-every-run) | Recursive chown/stat of the entire ~/.nvm tree on every run | performance | small | — |  |
| [DOC-01](research/docs.md#doc-01-docker-documented-as-rootlessoptional-it-is-rootful-and-core) | Docker documented as rootless/optional — actually rootful and core | docs | medium | ✅ confirmed (high), severity adjusted high→medium |  |
| [DOC-02](research/docs.md#doc-02-ansible-vault-editview-instructions-are-broken-variable-level-encryption) | ansible-vault edit/view instructions are broken (variable-level encryption) | docs | small | ✅ confirmed (high), severity adjusted high→medium |  |
| [DOC-03](research/docs.md#doc-03-docsnordvpn-installationmd-documents-a-playbook-and-implementation-that-no-longer-exist) | nordvpn-installation.md documents a removed playbook and implementation | docs | medium | ✅ confirmed (high), severity adjusted high→medium |  |
| [DOC-04](research/docs.md#doc-04-claudeplanworkflowmd-describes-a-different-projects-qa-and-planning-infrastructure) | CLAUDE/PlanWorkflow.md describes the hooks-daemon project, not this repo | docs | medium | ✅ confirmed (high), severity adjusted high→medium |  |
| [DOC-05](research/docs.md#doc-05-docsplaybooksmd-catalogue-badly-misclassifies-core-vs-optional-and-omits-25-playbooks) | playbooks.md catalogue misclassifies core vs optional and omits ~25 playbooks | docs | large | — |  |
| [DOC-06](research/docs.md#doc-06-docsarchitecturemd-execution-flow-lists-9-of-24-core-playbooks) | architecture.md execution flow lists 9 of 24 core playbooks | docs | small | — |  |
| [DOC-07](research/docs.md#doc-07-stale-fedora-42-references-on-the-f43-branch) | Stale Fedora 42 target references on the F43 branch | docs | small | — |  |
| [DOC-08](research/docs.md#doc-08-claudegnomeshellmd-describes-an-obsolete-speech-to-text-pipeline) | CLAUDE/GnomeShell.md describes obsolete speech-to-text pipeline (rec/wsi-transcribe/whisperfile/wtype) | docs | small | — |  |
| [DOC-09](research/docs.md#doc-09-docscontainerizationmd-omits-podman-entirely-and-calls-ccy-docker-based) | containerization.md omits Podman entirely and calls CCY Docker-based | docs | medium | — |  |
| [DOC-10](research/docs.md#doc-10-claudeqamd-misstates-how-the-qa-scripts-behave) | CLAUDE/QA.md misstates qa-ansible wiring and ruff/shellcheck requirements | docs | small | — | QA-11 |
| [DOC-11](research/docs.md#doc-11-docsccy-debug-mountsmd-is-a-stale-session-log-with-broken-instructions) | ccy-debug-mounts.md is a stale session log with a broken token path | docs | small | — |  |
| [OPP-01](research/opportunities.md#opp-01-stray-tracked-artefacts-at-repo-root-localhost-loclahost-and-a-u00a0-named-file-leaking-a-home-directory-listing) | Stray tracked artefacts at repo root: localhost, loclahost, and a U+00A0-named file leaking a home-directory listing | opportunities | small | — | QA-03 |
| [OPP-02](research/opportunities.md#opp-02-qa-gate-has-no-ansible-syntax-validation-despite-documented-219-parser-gotchas) | QA gate has no Ansible syntax validation despite documented 2.19 parser gotchas | opportunities | medium | — | QA-02 |
| [OPP-03](research/opportunities.md#opp-03-no-automated-test-coverage-for-ccy-the-highest-risk-bash-in-the-repo) | No automated test coverage for CCY — 6,600+ lines of high-risk bash | opportunities | large | — |  |
| [OPP-04](research/opportunities.md#opp-04-plan-index-drift-implemented-work-still-listed-as-active-three-overlapping-claude-devtools-plans) | Plan index drift: implemented work still listed Active; three overlapping claude-devtools plans | opportunities | medium | — |  |
| [QA-01](research/qa-gaps.md#qa-01-ccy-version-bump-pre-commit-check-is-dead-code-broken-grep-pipeline) | CCY version-bump pre-commit enforcement is dead code (broken grep pipeline) | qa-gaps | small | ✅ confirmed (high), severity adjusted high→medium | BSH-01 |
| [QA-02](research/qa-gaps.md#qa-02-no-yamlansible-syntax-validation-anywhere-in-qa---syntax-check-never-integrated) | No YAML/Ansible syntax validation in QA; ansible-playbook --syntax-check integrated nowhere | qa-gaps | medium | ✅ confirmed (high), severity adjusted high→medium | OPP-02 |
| [QA-05](research/qa-gaps.md#qa-05-qa-bash-scans-upstreamvendorruntime-files-70-of-scanned-files-and-87-of-shellcheck-noise-are-out-of-scope) | qa-bash scans upstream/vendor/runtime code: 70% of scanned files are .claude/hooks-daemon, roles/vendor, ccy shell-snapshots | qa-gaps | small | — | PERF-01 |
| [QA-06](research/qa-gaps.md#qa-06-qa-ansiblebash-fail-fast-grep-scope-is-too-narrow) | qa-ansible.bash fail-fast grep misses tasks/, vars/, environment/, *.yaml, and 'ignore_errors: True'; results absent from QA JSON | qa-gaps | small | — |  |
| [QA-07](research/qa-gaps.md#qa-07-semgrep-fail-fast-ruleset-has-exactly-one-rule-most-prohibited-error-hiding-patterns-unchecked) | Semgrep fail-fast ruleset is a single '\|\| echo' rule — '\|\| true', '\|\| :' and stderr-silencing unchecked repo-wide | qa-gaps | medium | — |  |
| [QA-08](research/qa-gaps.md#qa-08-qa-scripts-themselves-swallow-tool-crashes-fail-fast-violations-inside-the-gate) | QA scripts swallow analyser crashes with '\|\| true' (ruff/shellcheck crash reported as pass) and ruff --fix mutates files during checks | qa-gaps | small | — | BSH-13, FF-04 |
| [QA-09](research/qa-gaps.md#qa-09-existing-pytest-suites-are-never-executed-by-any-qa-gate) | Existing pytest suites are never executed by any QA gate | qa-gaps | small | — |  |
| [QA-13](research/qa-gaps.md#qa-13-secret-scanning-hooks-have-pattern-and-logic-blind-spots) | Secret-scan hooks miss private-key blocks and common token formats; whole-file whitelist logic can mask real /home leaks | qa-gaps | medium | — | SEC-02 |

## Low — polish / batch up (50)

Genuine but minor; best fixed in batches alongside related work.

| ID | Title | Area | Effort | Verified | Related |
|----|-------|------|--------|----------|---------|
| [SEC-04](research/security.md#sec-04-mok-enrollment-password-echoed-in-plaintext-to-the-terminallogs) | MOK enrollment password echoed in plaintext to terminal/logs | security | small | — |  |
| [SEC-05](research/security.md#sec-05-unpinned-curl-bash-installers-without-checksum-or-signature-verification) | Unpinned curl\|bash installers without checksum or signature verification | security | medium | — |  |
| [SEC-06](research/security.md#sec-06-disable_gpg_check-true-on-several-third-party-rpm-installs) | disable_gpg_check: true on several third-party RPM installs | security | medium | — |  |
| [FF-06](research/fail-fast.md#ff-06-ccy-entrypoint-github-known_hosts-fetch-fails-completely-silently) | CCY entrypoint: GitHub known_hosts fetch fails completely silently | fail-fast | small | — | BSH-16, CCY-08 |
| [FF-07](research/fail-fast.md#ff-07-shutdown-with-update-no-set--e-firmware-failures-warn-and-continue) | shutdown-with-update: no set -e; firmware refresh failure warn-and-continue | fail-fast | small | — |  |
| [FF-08](research/fail-fast.md#ff-08-diagnostic-scripts-lack-strict-mode-and-any-design-annotation) | Diagnostic scripts lack strict mode and any design annotation | fail-fast | small | — |  |
| [FF-09](research/fail-fast.md#ff-09-wsi--python-family-40-except-exception-pass-blocks-some-on-data-paths) | wsi-* Python family: ~40 except Exception: pass, some on data-path reads | fail-fast | medium | — |  |
| [FF-10](research/fail-fast.md#ff-10-qa-tooling-pattern-gaps-true-uncovered-qa-ansible-regexscope-gaps) | QA tooling pattern gaps: \|\| true uncovered; qa-ansible regex and scope gaps | fail-fast | small | — |  |
| [ANS-13](research/ansible.md#ans-13-40-commandshell-tasks-lack-changed_when-changed-status-reporting-is-meaningless-native-modules-bypassed) | ~40 command/shell tasks lack changed_when — changed reporting meaningless; native modules bypassed | ansible | medium | — |  |
| [ANS-14](research/ansible.md#ans-14-15-file-deploying-tasks-missing-mode-and-mostly-ownergroup) | 15 file-deploying tasks missing mode (and mostly owner/group) | ansible | small | — |  |
| [ANS-15](research/ansible.md#ans-15-duplicate-shebang-line-in-16-playbooks) | Duplicate shebang line in 16 playbooks | ansible | small | — |  |
| [ANS-16](research/ansible.md#ans-16-unpinned-checksum-less-binary-downloads-rm--rf-glob-in-downloads-version-gated-installer-logic-duplicated) | Unpinned checksum-less binary downloads and rm -rf globs in ~/Downloads; installer logic duplicated | ansible | medium | — |  |
| [BSH-15](research/bash.md#bsh-15-pre-commit-secret-scan-unquoted-file-loop-and-true-on-the-staged-file-list) | pre-commit secret scan: unquoted file loop skips filenames with spaces; git failure passes the gate | bash | small | — |  |
| [BSH-16](research/bash.md#bsh-16-entrypointsh-github-known_hosts-fetch-silently-optional) | entrypoint.sh fetches GitHub known_hosts silently — offline failure surfaces later as opaque SSH errors | bash | small | — | CCY-08, FF-06 |
| [BSH-17](research/bash.md#bsh-17-check-displaylink-statussh-glob-test-breaks-with-multiple-evdi-dirs---check-mode-is-not-silent) | check-displaylink-status.sh: -d with glob breaks on multiple evdi versions; --check mode is not silent | bash | small | — |  |
| [BSH-18](research/bash.md#bsh-18-scriptsvault-pervasive-legacy-shellcheck-debt-unquoted-cd-substitutions-unchecked-cd-cross-file-variables) | scripts/vault/: shared prologue unquoted and unchecked (cd without exit) plus 100+ legacy shellcheck warnings | bash | medium | — |  |
| [CCY-04](research/ccy.md#ccy-04-gh-active-account-switching-has-no-interrupt-safe-restore) | gh active-account switching lacks interrupt-safe restore | ccy | small | — |  |
| [CCY-05](research/ccy.md#ccy-05-dockerfile-claims-a-runtime---user-mapping-the-wrapper-never-provides) | Dockerfile claims a runtime --user mapping the wrapper never provides | ccy | small | — |  |
| [CCY-06](research/ccy.md#ccy-06-token-byte-length-message-inconsistent-with-the-accepted-range) | Token byte-length message inconsistent with accepted range | ccy | small | — |  |
| [CCY-08](research/ccy.md#ccy-08-known_hosts-population-in-the-container-silently-continues-on-failure) | Container known_hosts population silently continues on failure | ccy | small | — | BSH-16, FF-06 |
| [EXT-05](research/extensions.md#ext-05-synchronous-file-io-on-the-gnome-shell-main-thread) | Synchronous file I/O on the GNOME Shell main thread (per-line debug logging, sync reads) | extensions | small | — |  |
| [EXT-06](research/extensions.md#ext-06-disable-during-an-active-recording-leaks-the-detached-_iconbox-and-skips-countdown-teardown) | disable() during active recording orphans the detached _iconBox and skips countdown teardown | extensions | small | — |  |
| [EXT-07](research/extensions.md#ext-07-article-mode-spinner-can-animate-forever-if-the-helper-fails-after-launch-_elapsedseconds-is-dead-state) | Article-mode spinner runs indefinitely if the helper dies without a DBus signal; _elapsedSeconds is dead code | extensions | small | — |  |
| [EXT-08](research/extensions.md#ext-08-workspace-names-overview-verified-against-gnome-487-but-shipped-for-49-plus-deprecated-schema-construct-property) | workspace-names-overview private-API paths verified against 48.7 but shipped for GNOME 49; deprecated Gio.Settings 'schema' property | extensions | small | — |  |
| [EXT-09](research/extensions.md#ext-09-stale-generated-binary-gschemascompiled-tracked-in-git) | Stale generated gschemas.compiled binary tracked in git | extensions | small | — |  |
| [EXT-10](research/extensions.md#ext-10-documentation-contradictions-in-extensionsclaudemd) | extensions/CLAUDE.md contradicts QA.md (npm run lint) and documents an outdated structure | extensions | small | — |  |
| [EXT-12](research/extensions.md#ext-12-remote-desktop-toggle-async-callback-can-touch-the-toggle-after-destroy) | remote-desktop-toggle async subprocess callbacks can touch the toggle after destroy() | extensions | small | — |  |
| [EXT-13](research/extensions.md#ext-13-workspace-names-deployment-copies-all-files-with-mode-0755-and-no-ownergroup) | workspace-names deployment copies all files with mode 0755 and no owner/group | extensions | small | — |  |
| [PERF-06](research/performance.md#perf-06-play-pythonyml-performs-pypi-network-work-on-every-run) | play-python.yml: pipx state:latest x3 plus a redundant unguarded 'pdm self update' hit PyPI every run | performance | small | — |  |
| [PERF-07](research/performance.md#perf-07-unguarded-every-run-networksystem-commands-in-core-plays-flatpak-firewalld) | Unguarded flatpak install and firewalld reload run on every main run | performance | small | — |  |
| [PERF-08](research/performance.md#perf-08-play-rust-devyml-repeats-toolchain-updates-and-duplicates-a-component-task) | play-rust-dev.yml repeats rustup network updates and duplicates the rust-analyzer component task | performance | small | — |  |
| [PERF-09](research/performance.md#perf-09-gnome-extension-installer-queries-extensionsgnomeorg-seven-times-per-run) | Seven extensions.gnome.org HTTP requests per main run from the GNOME extension installer loop | performance | small | — |  |
| [PERF-10](research/performance.md#perf-10-extensions-use-timer-polling-where-event-driven-apis-exist) | Extensions poll on fixed timers where event-driven APIs exist | performance | medium | — |  |
| [PERF-11](research/performance.md#perf-11-wsi-stream-server-killed-on-every-speech-to-text-play-run) | wsi-stream server killed unconditionally on every speech-to-text play run | performance | small | — |  |
| [DOC-12](research/docs.md#doc-12-broken-relative-links-and-anchors) | Broken relative links and anchors in docs and CLAUDE files | docs | small | — |  |
| [DOC-13](research/docs.md#doc-13-docsreadmemd-index-omits-five-docs-and-contains-wrong-quick-reference-paths) | docs/README.md index omits five docs and has wrong quick-reference paths | docs | small | — | OPP-11 |
| [DOC-14](research/docs.md#doc-14-docsdevelopmentmd-references-a-nonexistent-pre-commit-setup-and-omits-the-qa-gate) | development.md references nonexistent pre-commit framework and omits the QA gate | docs | small | — |  |
| [DOC-15](research/docs.md#doc-15-stale-playbook-paths-in-directory-guides) | Stale playbook paths in configuration.md and playbooks/CLAUDE.md | docs | small | — |  |
| [DOC-16](research/docs.md#doc-16-docsfeaturesreadmemd-coming-soon-lists-docs-that-already-exist) | features/README.md 'Coming Soon' lists docs that already exist | docs | small | — |  |
| [DOC-18](research/docs.md#doc-18-ctrlz-patch-cross-references-and-missing-native-binary-mode-in-claudecontainerrulesmd) | ctrl+z patch cross-references stale; native-binary mode undocumented in ContainerRules.md | docs | small | — |  |
| [OPP-06](research/opportunities.md#opp-06-dry-resolve-latest-github-release-implemented-four-different-ways-across-six-playbooks) | DRY: 'resolve latest GitHub release' implemented four ways across six playbooks | opportunities | medium | — |  |
| [OPP-07](research/opportunities.md#opp-07-dry-bash-colour-palette-and-log-helper-boilerplate-duplicated-in-10-scripts) | DRY: bash colour palette and log helpers duplicated in 10+ scripts | opportunities | small | — |  |
| [OPP-08](research/opportunities.md#opp-08-orphaned-deployed-file-filesusrlocalbindebug-pipewirebash-is-tracked-but-never-deployed) | Orphaned deployed file: files/usr/local/bin/debug-pipewire.bash never deployed by any playbook | opportunities | small | — |  |
| [OPP-09](research/opportunities.md#opp-09-runbash-prints-a-stale-path-for-the-python-playbook) | run.bash prints stale path to play-python.yml in end-of-run guidance | opportunities | small | — |  |
| [OPP-10](research/opportunities.md#opp-10-unreferencedunder-documented-helper-scripts) | Unreferenced or misleadingly named helper scripts (setup-rclone.bash, test-ccy-ssh-probe.bash, desktop-symlinks) | opportunities | small | — |  |
| [OPP-11](research/opportunities.md#opp-11-docsreadmemd-index-missing-four-documents-a-planning-doc-stranded-in-docs) | docs/README.md index missing four documents; stale planning doc stranded in docs/ | opportunities | small | — | DOC-13 |
| [OPP-13](research/opportunities.md#opp-13-extensions-dx-empty-test-scaffold-and-no-hooks-compliant-lint-script) | Extensions DX: empty test scaffold and no hooks-compliant llm:lint script | opportunities | small | — |  |
| [QA-10](research/qa-gaps.md#qa-10-javascript-qa-is-manual-partial-and-ccy-ctrl-z-patchjs-has-no-lint-coverage) | JavaScript QA is manual and partial; ccy-ctrl-z-patch.js has no lint coverage; ESLint 8 is EOL | qa-gaps | small | — | EXT-01 |
| [QA-11](research/qa-gaps.md#qa-11-claudeqamd-misdescribes-the-suite) | CLAUDE/QA.md misdescribes the suite (qa-ansible 'via qa-patterns', omitted from run table, advisory behaviours undocumented) | qa-gaps | small | — | DOC-10 |
| [QA-12](research/qa-gaps.md#qa-12-qa-bash-output-polish-bugs) | qa-bash points users at a deleted temp file for shellcheck diagnostics and emits null-byte warnings on binaries | qa-gaps | small | — |  |

## Info — observations and opportunities (13)

Not defects. Observations, design notes, and improvement ideas.

| ID | Title | Area | Effort | Verified | Related |
|----|-------|------|--------|----------|---------|
| [FF-11](research/fail-fast.md#ff-11-ssh-handlingbash-token-owner-cross-check-silently-skipped-when-github-api-unreachable) | ssh-handling.bash: token-owner cross-check silently skipped when GitHub API unreachable | fail-fast | small | — |  |
| [ANS-17](research/ansible.md#ans-17-nvm-grep-guard-prevents-the-managed-bashrc-block-from-ever-updating) | NVM grep-guard prevents the managed bashrc block from ever updating | ansible | small | — |  |
| [ANS-18](research/ansible.md#ans-18-lastpass-login-task-contains-duplicated-dead-jinja-branches) | LastPass login task contains duplicated dead Jinja branches | ansible | small | — |  |
| [BSH-19](research/bash.md#bsh-19-vendored-gnome-shell-extension-installer-carries-genuine-quoting-errors) | Vendored gnome-shell-extension-installer carries error-level quoting bugs | bash | small | — |  |
| [CCY-07](research/ccy.md#ccy-07-ctrlz-patch-soft-fail-is-silent-in-the-build-stream) | ctrl+z patch soft-fail is only a build-log warning | ccy | small | — |  |
| [CCY-09](research/ccy.md#ccy-09-update_claude_inplace-leaks-the-temp-container-on-commit-failure) | update_claude_inplace leaks temp container on commit failure | ccy | small | — |  |
| [CCY-10](research/ccy.md#ccy-10-oauth-and-gh-tokens-passed-via-container-env-vars) | OAuth and gh tokens passed via container env vars | ccy | small | — |  |
| [EXT-11](research/extensions.md#ext-11-no-automated-tests-for-any-extension-eslint-8-is-end-of-life) | No automated tests for any extension; ESLint 8 is end-of-life | extensions | medium | — |  |
| [PERF-12](research/performance.md#perf-12-two-large-pdfs-tracked-in-git-37-of-pack-size) | Two vendor PDFs (~1.8 MB) tracked in git under a plan folder | performance | small | — |  |
| [DOC-17](research/docs.md#doc-17-docsansible-lint-improvement-planmd-is-a-stale-plan-document-living-in-docs) | ansible-lint-improvement-plan.md is a stale plan document in docs/ | docs | small | — |  |
| [OPP-05](research/opportunities.md#opp-05-abandoned-plans-worth-an-explicit-revive-or-cancel-decision) | Abandoned plans needing an explicit revive-or-cancel decision (023, 027, 014, 002, 004, 007) | opportunities | small | — |  |
| [OPP-12](research/opportunities.md#opp-12-play-unifi-controlleryml-hardcodes-podman-instead-of-the-container_engine-variable) | play-unifi-controller.yml hardcodes podman instead of the container_engine variable | opportunities | small | — | ANS-05 |
| [QA-14](research/qa-gaps.md#qa-14-file-classes-with-zero-qa-and-an-acknowledged-but-unimplemented-playbook-check) | Uncovered file classes: Dockerfiles, markdown links, JSON validity; playbook shebang/exec-bit check documented but never implemented | qa-gaps | small | — |  |

## Coverage gaps — follow-up research candidates

The completeness critic identified six areas the audit did not cover. Details in [research/coverage-gaps.md](research/coverage-gaps.md):

1. **fedora-install/ bootstrap entirely unaudited (2,770 lines of security-critical install code)** — Zero findings reference fedora-install/ despite it handling the highest-trust phase of the system lifecycle.
2. **run.bash automated failure-report flow posts hostname and weakly sanitised logs to the PUBLIC repo's issues** — run.bash (1,470 lines, the primary entry point) appears in only one finding (OPP-09, a stale path).
3. **.gitignore correctness never audited — confirmed malformed line breaks the !.env.dist negation** — Line 49 of .gitignore is literally `!.env.dist# Security incident documentation - DO NOT COMMIT` — a missing newline merged the negation pattern with the next section's comment header.
4. **Tracked .claude/ project-level custom code unaudited (42 tracked files incl. two Python hook handlers and a second CCY Dockerfile)** — The exclusion list covered only .claude/hooks-daemon/ (upstream), but the repo tracks 42 files under .claude/ that are project-owned code: custom pre_tool_use handlers ansible_enforcement.py (248 lines) and system_paths.py (185 lines) with their tests, agent definitions, settings.json hook wiring, and .claude/ccy/Dockerfile — a second, distinct Dockerfile (extends claude-yolo with ansible/pipx) separate from files/var/local/claude-yolo/Dockerfile that the CCY dimension audited.
5. **Galaxy dependency supply chain: requirements.yml pins the vault-scripts role to 'master' and collections are unversioned** — requirements.yml fetches LongTermSupport/ansible-role-vault-scripts at `version: master` and community.general / ansible.posix with no version at all; roles/vendor/* is gitignored, so despite the 'vendored roles' label nothing is actually vendored — every fresh install pulls whatever master currently is.
6. **Licensing compliance unexamined: GPL-2.0 script redistributed inside an MIT-licensed repo** — Minor but unrepresented risk class.

## Research document index

| Dimension | Document | Findings |
|-----------|----------|----------|
| Security | [research/security.md](research/security.md) | 6 |
| Fail-Fast | [research/fail-fast.md](research/fail-fast.md) | 11 |
| Ansible Correctness | [research/ansible.md](research/ansible.md) | 18 |
| Bash Quality | [research/bash.md](research/bash.md) | 19 |
| CCY Container System | [research/ccy.md](research/ccy.md) | 10 |
| GNOME Extensions | [research/extensions.md](research/extensions.md) | 13 |
| Performance | [research/performance.md](research/performance.md) | 12 |
| Documentation Drift | [research/docs.md](research/docs.md) | 18 |
| Opportunities | [research/opportunities.md](research/opportunities.md) | 13 |
| QA & Tooling Gaps | [research/qa-gaps.md](research/qa-gaps.md) | 14 |
| Coverage gaps | [research/coverage-gaps.md](research/coverage-gaps.md) | 6 |
