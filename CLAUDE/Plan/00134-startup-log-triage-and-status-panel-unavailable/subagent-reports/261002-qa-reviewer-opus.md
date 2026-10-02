## QA Review: Plan 00134, full diff (c41403cf to 25657b76)

**Verdict: FIX-BEFORE-MERGE.** One plan-local triage blind spot needs fixing. Everything else is advisory.

### BLOCK
None.

### FIX-BEFORE-MERGE
1. **The AVC probe can't tell "no denials" from "sudo was refused"** (`triage.bash:254-257`). `sudo -n ausearch … | tee "$avcRaw" >/dev/null` sends the output only to the file. A refused sudo then prints `ausearch rc=1` and `AVC records this boot: 0`, which is exactly what a clean boot prints. The rc=1 hint on line 256 covers both cases. The downstream aggregations (lines 269-330) would then be empty without saying so. Sudo is primed once at line 78, but its timestamp can expire while the long journal probes run. The one open Success Criterion (`ausearch … container_t … bounded`, PLAN.md:178) and Task 3.1's host check read this probe. Fix: when grep finds no `<no matches>` and no `type=AVC`, say so and return non-zero.
2. **Task 4.1 is agent work, not a host check** (PLAN.md:162). Running qa-all and ESLint can be done in the container. I ran ESLint on `sections/health.js` (clean) and `test-panel-sections.bash` (126 passed), but not the full qa-all. Tasks 4.1 and 4.2 are the only open tasks that are agent work. Every other open task is genuinely a host verification: 2.1 wpctl, 2.2 and 2.6 next-boot journal, 2.5 next login, 3.1 deploy plus ausearch.

### ADVISORY
3. **`abrt-prune-stale.bash` has no test** (`files/usr/local/bin/abrt-prune-stale.bash:32-44`). It parses `abrt-cli` output, and its "No problems" bug reached the host before e5fee0e3 fixed it. Nothing under `tests/` or `scripts/` references it, so that regression is unguarded.
4. **The repo file the plan now keeps is guarded only by existence** (`play-browsers.yml:237`). `creates: /etc/yum.repos.d/vivaldi-fedora.repo` predates the plan, but the plan now relies on that file alone. A zero-byte file would suppress the fetch permanently (the AgentNotes Plan 00067 shape).
5. **The DisplayLink logrotate fix is not in PLAN.md** (624335ec, `play-displaylink.yml:151-159`). It landed under 00134 and is recorded only in the journals. It needs a task line, or a note naming the plan that owns it.
6. **c41403cf deletes `.ruff-version` without saying so.** The deletion itself is correct: `.qa-versions` has replaced it and nothing live reads it (`scripts/qa-python.bash:48`). But the triage commit's message doesn't mention it.
7. **1a278464's message contradicts its diff.** It says gawk is "the next commit", but this diff already adds gawk to `.claude/ccy/Dockerfile`.
8. **plan-qa reports journal entries out of order** in `JOURNAL/00134-Journal-26-09-23.md` (19:10 after 19:20). This is advisory only; the journal is append-only, so leave it as it is.

### Checked and clean
- **2.1 WirePlumber:** the SPA-JSON now matches node names for node properties. The play removes its own Lua and stops (assert at `play-hd-audio.yml:377`) on any Lua it didn't write.
- **2.2 Vivaldi:** `repo_add_once="false"` plus removing the duplicate file every run is idempotent.
- **2.3 vmtest-bridge:** `TimeoutStartSec=120` is in place, and `RuntimeMaxSec` now appears in `files/` only in that unit's comment.
- **2.4 Toolbox:** a stat, then a 0644 mode task gated on the file existing, with no `creates:`. The `~` paths follow the play's convention.
- **2.5 ABRT:** desktop-only, installs its own packages, avoids the 2.19 self-default trap through the resolve step, and uses `blockinfile` with `create: false`. The script's stdout carries only its marker line.
- **2.6 Thunar:** `state: absent` in the packages section, documented.
- **3.1 relabel:** the verdict maps Permissive to `permissive`. The preflight fails closed on an unreadable uid map or a failed walk, asks at most three times, and stops when there is no TTY. Bumps 3.65.0, 3.65.1, 3.68.0 and 3.69.1 all touch the launcher. LABEL and `REQUIRED_CONTAINER_VERSION` agree at 2.38. The image-baked Dockerfile is untouched; the gawk change is in the project's `.claude/ccy/Dockerfile`, which needs no bump.
- **3.2 Docker:** the backend assert runs before the DOCKER-USER chain assert and fails fast when the backend line is missing.
- **Play ledger (1.1/1.6):** the ad-hoc and console paths are skipped. A playbook play with no position still marks the ledger broken. The real-Ansible test ran rather than skipped.
- **Public repo:** no home paths, emails, private IPs or hex IDs in the plan folder. The placeholders `container-A` and `project A/B` are used. `Owner: joseph` is the repo-wide convention (99 plans).
- **Docs:** `docs/ccy.md:403-414`, `docs/playbooks.md:1071-1074` and `docs/configuration.md:178-200` match the code.

### Mechanical gates
- **Full qa-all.bash:** not run, as you asked.
- **Targeted tests:** `test-ccy-relabel-preflight` 57/0 and `test-ccy-selinux-verdict` 16/0. Pytest over `play_ledger` and `host_health`: 626 passed, 1 skipped (a root-only case).
- **plan-qa --sweep:** 0 block, 11 advise. For 00134 that is item 8 plus "7 days since last journal entry".
- **syntax-check:** OK for all seven playbooks the plan changed (hd-audio, browsers, toolbox, basic-configs, lxc, claude-yolo, displaylink).
- **I broke the read-only rule once:** my syntax-check loop wrote its stderr to `/workspace/untracked/scratch/.qa-syn.err`. It is an untracked scratch file and nothing tracked changed. You can delete it.
- **Uncommitted changes:** the edits in `helpers/containerwatch/cli.py` and `helpers/gnome/check_panel_contract.py` (plus its test) are not part of 00134 and were not reviewed.