# Read-only triage scan: In Progress plans, container-doable vs blocked

Branch `F44`. Read-only — nothing changed. 28 plans inspected (00109, 00112, 00122, 00124 skipped as already-established blocked).

## Per plan

**00037 image-watermarking-toolkit** — 33 open. **CONTAINER-DOABLE**: Task 2.2 (write `tests/watermark/` fixture + golden/idempotency/force/missing-config test script), Task 3.2 (`docs/watermark.md` wrapping guide), Task 3.3 (decide `--stdin0`; DECISIONS.md Decision 4 says "likely YES"). Tasks 1.1/1.2/2.3/5.3/5.4 are host (`dnf info`, eyeballing real photos). **Drift**: Task 5.2 "Commit" is unticked but `4cc572e9` already landed the script, configs and playbook.

**00040 raw-clipping-scanner** — 17 open. **BLOCKED**: every open item is a host probe (`dnf info python3-rawpy`, `pip show rawpy`), a real Sony A7V ARW sample set, a 500-frame benchmark, or the playbook idempotency run. Physical camera files + host.

**00041 remote-desktop-quick-toggle** — 17 open. **BLOCKED**: Phase 2 is entirely host Ansible + a live GNOME/Wayland session + a second laptop over RDP; Phase 3 is explicitly "Optional" polish gated on Phase 2 landing.

**00046 localhost-yml-leak-guard** — 24 open. **BLOCKED — owner decision**. The plan header itself says "Possibly superseded", because the daemon now ships `sensitive_content` (`R-SENSITIVE-PUBLIC-PATTERN`/`R-SENSITIVE-SECRET-TERM`) covering most of the scope; the residual gap is auto-deriving the deny-list from `localhost.yml`. Archive-vs-build is not settled by any repo fact. Phases 2–3 (pytest-first handler) become container-doable the moment the owner says "build".

**00048 cc-token-source-parity** — 16 open. **CONTAINER-DOABLE (plan hygiene only)**: Tasks 5.3/5.4 are a status flip and "commit code + plan together" — and the `cc` wrapper is already committed, so 5.4 is stale. All substantive open items (4.3, 4.6, 4.7, empty-pool sanity) need the host token pool and a `claude` binary.

**00049 full-repo-audit** — 8 open. **CONTAINER-DOABLE**: QA-09 ("add pytest stage, deferred — pytest missing from CCY image") is settled by a repo fact — `scripts/qa-helper-tests.bash` now runs every `helpers/` test on stdlib `unittest`, explicitly "no pytest/venv", and is wired into `qa-all.bash`. The item should be closed/reworded, not deferred. DOC-18 (Dockerfile cross-ref comment) is a one-line edit + version bump.

**00063 headless-run-bash-server-cloud-provisioning** — 10 open. **BLOCKED**: Task 3.2 needs a human at a terminal by definition; 3.3/3.4 need the `server-github-token` vmtest scenario run on the HOST with a real scoped PAT on a throwaway account.

**00075 fail-signal-discard-sweep-and-gate** — 1 open. **CONTAINER-DOABLE**: Task 4.4 — file the upstream hooks-daemon report (`lint_on_edit` honours neither `options.exclude_paths` nor `daemon.exclude_paths`; `.claude/init.sh:310` `resolve_venv_python` returns 0 on failure). `gh` is authed in this container as `<gh-user>` with `repo` scope, so the access exists. Outward-facing, so it wants owner go-ahead before posting.

**00076 bash-gate-coverage-hole-nonexecutable-scripts** — 1 open. **CONTAINER-DOABLE**: Task 4.5 — the patterns gate prints `✓ patterns: 157 files OK`, which is the union across rules; `bash-capture-discards-status` actually saw 90 of 157. Pure `scripts/qa-patterns.bash` work. Nominally "AWAITING A DECISION" between A (one scan per rule, +14s, measured) and B (model the globs in-gate), but the plan already records B's blocker as refuted, rules out `--time match_times` by measurement, and states a reasoned preference for A.

**00080 ccy-session-network-isolation** — 14 open. **CONTAINER-DOABLE (one task)**: Task 4.0 — document that `--no-network` is the *isolating* mode; F2b is already established in Phase 1 research, and `docs/ccy.md` plus the `--help` line currently describe it only as "skip auto-detection". Everything else is blocked: Task 1.3 needs `triage.bash` re-run on the HOST for P13, and the Task 2.2 decision gate waits on that data.

**00082 run-bash-github-accounts-none** — 4 open. **BLOCKED**: Phase 5 is a host `--list-tasks`/full run plus an end-to-end `GITHUB_ACCOUNTS=none` provision on a real Fedora guest, already in flight from a downstream consumer. **Drift**: the plan says the branch is "local, not yet pushed", but `origin/task/run-bash-github-accounts-none` exists.

**00086 kernel-modules-absent-enumeration** — 2 open. **BLOCKED**: Task 1.9 needs an external repo's live-proof harness re-pinned and re-run on a guest lacking `kernel-modules` — VM/host, and not this repo.

**00088 claude-code-state-dir-stale-home-fact** — 2 open. **BLOCKED**: same shape — Task 1.6 is a downstream live-proof re-run on a guest.

**00089 ssh-handling-runner-token-guard** — 2 open. **BLOCKED**: the single open sub-item is explicitly "(On HOST, not in CCY container) rebuild the CCY image and prove a `--no-ssh` launch with a pre-set `GH_TOKEN`".

**00098 encrypted-claude-transcripts-at-rest** — 23 open. **CONTAINER-DOABLE**: Phase 1 has nine open build tasks that are all authoring — 1.3 `/etc/claude-code/managed-settings.json`, 1.4 bake it into the image + entrypoint assert, 1.5 credential-read-blocker handler (test first), 1.6 `CACHEDIR.TAG`, 1.7 age keypair play, 1.8 `ccy-seal`, **1.9 `lib/state-hygiene.bash` with an `assert_scanner_can_see()` canary**, 1.10 banner wiring, 1.11 install loop, 1.12 user timer. Tasks 0.2/0.4/0.5/0.6 are host probes; Phase 2/3 are gate-closed.

**00099 rclone-rc-auth-broke-unmigrated-clients** — 2 open. **CONTAINER-DOABLE**: Task 5.6 — run the `qa-reviewer` agent over the full diff. Read-only, one task, no host needed. (Success criterion "qa-reviewer returns PASS" is the same item.)

**00114 run-bash-single-play-mode** — 2 open. **BLOCKED**: Task 3.3 needs an operator typing a become password at a terminal on a password-sudo box; the plan already says the prompt half is proven and the typed completion "is the operator's".

**00115 playbook-shebang-runs-through-run-bash** — 2 open. **BLOCKED**: Task 2.3 needs an estate guest rebuild or headless smoke to prove no shebang recursion — VM.

**00116 ccy-deploy-keys-and-forwarded-agent** — 4 open. **BLOCKED**: Task 3.3 needs a headless box holding only a deploy key, plus an `ssh -A` session for the forwarded-agent half.

**00118 ccy-selinux-enforcing-host** — 2 open. **BLOCKED**: Task 3.1 needs a live SELinux-**Enforcing** headless box with a deploy-key remote.

**00119 headless-github-ssh-443-input** — 2 open. **BLOCKED**: Task 2.1 needs a real port-22-blocked box, and the phase heading says it also waits on a consumer pinning the version.

**007 speech-to-text-resource-leak-fixes** — 88 "open", 0 ticked. **BLOCKED** on substance: Phase 5 needs a microphone, Firefox/Chrome, a live GNOME session; Phase 6 is a host Ansible deploy. **Contradiction (worst in the set)**: Phases 1–4 are written as `- [ ] ✅` — unchecked box carrying a done marker — throughout, and the Notes section records all three fixes as landed. That is why the count reads 88/0. The checkbox state is meaningless as written.

**009 claude-devtools** — 68 "open", 10 ticked. **BLOCKED** on substance: Phase 1 is host (`ghcr.io` pullability, `ss -tlnp | grep 3456`, inspecting a real CCY project dir) and Phase 6 is live host testing. **Contradiction**: parent tasks 2.1/3.1/4.1 are `[x] ✅` while every one of their subtasks is `[ ] ⬜`, and Task 5.1 is `[x] 🔄`. **Stale path**: Task 4.1 cites `play-install-claude-devtools.yml`, which does not exist — `45046e54` renamed it to `playbooks/imports/optional/common/play-claude-devtools.yml`. Container-doable work here is reconciliation only.

**014 whisper-model-manager** — 25 open. **BLOCKED**: Phase 5 only — run the Textual TUI in a terminal, download models, test the extension after a GNOME logout. Live session + network downloads. Checkbox state is internally consistent (Phases 1–4 genuinely ticked).

**018 fedora-kickstart-install** — **0 checkboxes of either kind**. **CONTRADICTORY / UNCLEAR**: Status is In Progress but the plan has no task list at all — Implementation Steps 1–6 are prose. All named artefacts exist (`fedora-install/ks.cfg`, `firstboot-setup.bash` content, `build-iso.bash`, `setup-netinstall-boot.bash`), and the repo has since gained `ks-vm-desktop.cfg`/`ks-vm-server.cfg` the plan never mentions. Step 6 (ksvalidator, VM test, real hardware) is host/VM. It also still targets F43 and recommends `ignore_errors: true` for `fwupdmgr`, which now violates the fail-fast HARD RULE. Container-doable work: convert to the plan template with real task state, or close it — no code work is identifiable from the file as it stands.

**022 install-security-and-resilience** — 52 open, 0 ticked. **CONTAINER-DOABLE**: Phases 1, 2, 3 and 5 are all edits to `fedora-install/ks.cfg` and `fedora-install/setup-netinstall-boot.bash` (adaptive `clearpart --list`, GRUB `password_pbkdf2` for the install entry, `%pre` auth gate, keymap/`loadkeys` fix). Decisions 1–4 are already recorded, so no decision gate blocks a start. Verification (`ksvalidator`, `grub2-mkfont` on host, real-hardware reinstall) is host. Caveat worth knowing before starting: the plan's own Phase 3 prescribes `sed`-substitution and Phase 4 prescribes `2>/dev/null … || echo "Warning: … soft-fail"`, both of which now violate current project rules and need re-specifying.

**026 repo-spring-cleaning** — 48 open. **CONTAINER-DOABLE**, and the findings are still live, not stale (verified this scan):

- `scripts/nvidia-status.bash` and `scripts/check-displaylink-status.sh` still have no `set -e`
- `state: latest` still in 5 playbooks — `play-AB-dnf-upgrade.yml`, `play-cloudflare-warp.yml`, `play-python.yml` (×4), `play-photography.yml`, `play-nvidia.yml`
- curl-piped-to-shell still in 5 playbooks — `play-nvm-install.yml:26`, `play-rust-dev.yml:45` and `:111`, `play-python.yml:72`, `play-claude-code.yml:87` — two more than the three the plan lists

Phases 3/5/6 are pure repo edits. Only sub-item 3.2 ("verify scripts still work with fail-fast") needs NVIDIA/DisplayLink hardware to exercise.

**032 compression-helpers** — 18 open. **BLOCKED** on substance: Phase 4 is host deploy + interactive compress/uncompress testing. **Drift**: the Phase 3 item "Commit with plan reference (awaiting user go-ahead)" is unticked, but `12deefe2` already committed both scripts and the playbook.

## Ranked CONTAINER-DOABLE

01. **00099** — Task 5.6: `qa-reviewer` over the full diff. One task, no design, read-only, closes the plan. Directly the QA/verification class.
02. **00076** — Task 4.5: per-rule coverage in the patterns gate. The pass line claims union coverage while `bash-capture-discards-status` is blind to 67 of 157 files — the exact "clean result indistinguishable from a blind one" defect, in the gate itself. One task, one file, option A already measured and argued.
03. **026** — Phases 3/5/6: fail-fast `set -e`, curl-pipe-to-shell → `get_url`, `state: latest` → `present`. Findings independently confirmed live. Mechanical, bounded, fail-fast class; no decision needed.
04. **00075** — Task 4.4: the upstream hooks-daemon report. Small and same defect class (`resolve_venv_python` returning 0 on failure is an instance of this plan's own class), but it posts externally, so it needs owner say-so.
05. **00098** — Task 1.9 first (`assert_scanner_can_see()` — "a blind scanner reports clean, which is indistinguishable from safety"), then 1.5/1.6/1.3. Highest continuity with 00125, but Phase 1 is nine tasks, not one.
06. **00049** — QA-09: the pytest deferral is answered by `qa-helper-tests.bash`; close the item and DOC-18. QA-gate subject, small, reconciliation rather than build.
07. **00080** — Task 4.0: document `--no-network` as the isolating mode. Tiny, fact already established, but doc-only and off the QA path.
08. **00037** — Tasks 2.2 / 3.2 / 3.3. Real test-coverage work, but Task 3.3 wants a decision first and the fixture generator wants `magick`.
09. **022** — Phases 1/2/3/5 on the kickstart. Large, wholly unstarted, and needs its own spec repaired (sed, soft-fail warnings) before implementing.
10. **009 / 018 / 032 / 00048** — plan-hygiene only: reconcile the `[ ] ✅` and ticked-parent/unticked-subtask checkbox states, the stale `play-install-claude-devtools.yml` path, and four "commit" tasks whose commits already exist. No code work available in-container.

## Cross-plan findings worth acting on

- **Four plans carry a "commit" task that is already committed**: 00037 (5.2 / `4cc572e9`), 032 (Phase 3 / `12deefe2`), 00048 (5.4), 009 (5.1). Plan state lags the code in all four.
- **Two plans have structurally broken checkbox state**: 007 uses `- [ ] ✅` for ~88 completed items; 009 ticks parents while leaving all subtasks unticked. Neither plan's completion percentage can be read from the file.
- **018 has no checkboxes at all** while marked In Progress, and targets F43.
- **00082's "not yet pushed" note is contradicted by `origin/task/run-bash-github-accounts-none`.**
- **`gh` is authed in this container** (`<gh-user>`, `repo` scope) — so "no gh access" is not a valid blocker for any plan here.

## Relevant paths

- `<repo>/CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/PLAN.md`
- `<repo>/CLAUDE/Plan/00076-bash-gate-coverage-hole-nonexecutable-scripts/PLAN.md`
- `<repo>/scripts/qa-patterns.bash`
- `<repo>/CLAUDE/Plan/026-repo-spring-cleaning/PLAN.md`
- `<repo>/CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/PLAN.md`
- `<repo>/CLAUDE/Plan/00075-fail-signal-discard-sweep-and-gate/PLAN.md`
- `<repo>/CLAUDE/Plan/007-speech-to-text-resource-leak-fixes/PLAN.md`
- `<repo>/CLAUDE/Plan/009-claude-devtools/PLAN.md`
- `<repo>/CLAUDE/Plan/018-fedora-kickstart-install/PLAN.md`
- `<repo>/CLAUDE/Plan/022-install-security-and-resilience/PLAN.md`
- `<repo>/scripts/qa-helper-tests.bash`
