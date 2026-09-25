# Completed Plans — Archive

Completed plans beyond the plan index's retention window of the most recent 30.
Rows are moved here **verbatim**: the wording is the wording that was written when
each plan closed, and rewriting it here would make this index disagree with the one
it was moved from. Links are relative to this directory.

Newest first, continuing from [../README.md](../README.md#completed-plans).

## Completed Plans

<!-- This heading is load-bearing, not decoration: the plan-QA row/folder bijection check
     classifies a row by the section it sits under, and without it all sixteen rows below
     were read as an unrecognised section while their folders were plainly completed —
     sixteen advisories saying so. -->

- [00071-qa-gate-correctness](00071-qa-gate-correctness/) - Fixed three defects that made `qa-all.bash` exit 1 on a clean tree, and pinned ruff via `/.ruff-version`

- [00067-qa-gates-inert-in-nested-checkout](00067-qa-gates-inert-in-nested-checkout/) - QA gates scanned nothing in a nested checkout; exclusions are now anchored to the repo root and each gate exits 2 on an empty file set

- [00060-stderr-hygiene-coding-standard](00060-stderr-hygiene-coding-standard/) - Fixed a `gh-<alias>()` wrapper that echoed status to stdout and shipped `CLAUDE/StderrHygiene.md` as the coding standard

- [00087-gitleaks-generic-key-false-positive](00087-gitleaks-generic-key-false-positive/) - Fixes a gitleaks CI false positive (`generic-api-key` on a "Medium/byteiota" source citation) by rephrasing the flagged text rather than growing `.gitleaks.toml`'s allowlist. Merged (`b15fc4d`).

- [00084-port-sudo-password-file-onto-f44](00084-port-sudo-password-file-onto-f44/) - Ports Plan 00073's `RUN_BASH_SUDO_PASSWORD_FILE` (stranded on an unmerged, diverged branch) onto `F44` so it composes with Plan 00082's `GITHUB_ACCOUNTS=none` — no single commit previously carried both. Merged (`d48fabd`). Also fixed two real VM hostnames from the downstream consumer estate that had been committed into this public repo's tracked content.

- [00083-plan-index-hygiene-and-comment-handlers](00083-plan-index-hygiene-and-comment-handlers/) - Enables the three handlers the 3.54.0 daemon upgrade shipped disabled (`comment_changelog`, `comment_size`, `sensitive_content`), each after measuring its existing backlog rather than assuming it, and clears the 39 over-length rows the new `index-row-length` check found in this index.

- [025-ccy-spring-cleaning](025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues

- [00050-fedora-44-tracking](00050-fedora-44-tracking/) - Fedora 43 → 44 migration tracking, research only: 55 findings across six version-sensitivity dimensions. The bump's core is one line, but seven highs gate it; execution deferred to a decision gate.

- [00077-ansible-inject-facts-as-vars-deprecation](00077-ansible-inject-facts-as-vars-deprecation/) - Converts the 11 live `ansible_<fact>` references that ansible-core 2.24 removes, and closes the hole with `qa-ansible.bash` Check 5. COMPLETE — proven on the host, after the first acceptance run certified nothing because it set an env var that does not exist.

- [00078-ccy-network-preflight-skip](00078-ccy-network-preflight-skip/) - Adds `CCY_SKIP_NETWORK_PREFLIGHT=1` so `ccy` can launch on an egress-fenced host, where the unconditional alpine-pull-and-HTTP liveness probe cannot pass by design. CCY 3.39.0.

- [00097-lightweight-agent-browser-engine](00097-lightweight-agent-browser-engine/) - Adds Lightpanda 0.3.6 as a complementary lightweight engine, reached through the `--engine` flag `agent-browser` already had: 379 ms / ~25 MB against Chromium's 1177 ms / ~1345 MB at identical fidelity on eight JS fixtures. Chromium stays the default because Lightpanda fails silently outside its scope.

- [00094-rclone-rc-auth-instead-of-no-auth](00094-rclone-rc-auth-instead-of-no-auth/) - Replaces blanket `--rc-no-auth` on the rclone RC — equivalent to shell access as the rclone user, and reachable by every local uid — with a host-generated 0600 secret loaded via systemd `EnvironmentFile=`. ACCEPTED on the host, 11 passed / 0 failed.

- [00057-lxc-net-networkmanager-bridge-race](00057-lxc-net-networkmanager-bridge-race/) - `lxc-net` failed at boot because an NM autoconnect profile claimed `lxcbr0` first, so dnsmasq never launched and containers never leased — while the play's own bridge check false-passed. Verified on the host: triage 9 failures → 0.

- [00096-docs-drift-repo-wide-fix](00096-docs-drift-repo-wide-fix/) - Audited every doc under `docs/` plus the root README against the real playbooks: 36 factual defects fixed, dominated by core plays documented as optional, and including two features documented that no task implements.

- [00095-document-ccy-system](00095-document-ccy-system/) - Shipped `docs/ccy.md` — the repo's daily driver had no user-facing documentation at all. A 10-agent adversarial pass caught six high-severity defects in the first draft, an invented flag among them.

- [00060-stderr-hygiene-coding-standard](00060-stderr-hygiene-coding-standard/) - A generated `gh-<alias>()` wrapper printed its status line on stdout, breaking `$(… --json)` captures. Fixed, audited repo-wide (0 other real bugs), and shipped `CLAUDE/StderrHygiene.md` as a coding standard.

- [00047-claude-code-mouse-wheel-pageup](00047-claude-code-mouse-wheel-pageup/) - The wheel clobbered the prompt in Claude Code's alt-screen renderer under `CLAUDE_CODE_DISABLE_MOUSE=1`. Path E shipped: drop the var so Claude Code captures the mouse and scrolls natively. Container 2.22, CCY 3.27.0.

- [00059-plan-folder-cleanup-and-plan-qa](00059-plan-folder-cleanup-and-plan-qa/) - Made `plan_workflow.qa` explicit in `.claude/hooks-daemon.yaml` and resolved the pre-existing plan-tree drift its first sweep surfaced (missing index rows, completed/cancelled plans left in the active root, missing status headers, a lowercase `plan.md`); `plan-qa --sweep` went 16 findings → 0.

- [002-nordvpn-openvpn-manager](002-nordvpn-openvpn-manager/) - `nord` bash script + Ansible playbook to manage NordVPN OpenVPN connections via NetworkManager (on-demand import, persistent connections, vault credentials). Shipped `files/home/.local/bin/nord`, `play-nordvpn-openvpn.yml`, and `docs/nordvpn-installation.md`.

- [006-documentation-audit-and-update](006-documentation-audit-and-update/) - Documentation coverage audit and feature inventory across the repo (coverage assessment + feature inventory supporting docs).

- [015-article-mode](015-article-mode/) - Article mode for speech-to-text: an indefinite looped recording mode that flushes every 120 s and re-polishes the whole raw article via Claude in a two-pane GTK window (`Shift+Insert`). Shipped `files/home/.local/bin/wsi-article` + `wsi-article-window`.

- [017-merge-ccy-ccb](017-merge-ccy-ccb/) - Retire CCB and CCB-Browser and consolidate into the single CCY tool.

- [021-firstboot-wizard-redesign](021-firstboot-wizard-redesign/) - Firstboot wizard redesign.

- [00052-run-bash-human-friendly](00052-run-bash-human-friendly/) - Made `run.bash` human-friendly (1.5.4 → 1.6.1): Enter accepts at every confirm, safe-polarity defaults, a visible `[default]` on every prompt, and verify-before-write hardening of the vault-password recovery path. HOST live run deferred to the user.

- [020-semgrep-custom-bash-rules](020-semgrep-custom-bash-rules/) - Add Semgrep with custom bash convention rules (no error hiding, fail-fast enforcement) integrated into qa-all.bash. Semgrep 1.153.1 installed via pipx in CCY Dockerfile (v2.10); 0 violations in 44 bash files.

- [024-claude-md-modular-restructure](024-claude-md-modular-restructure/) - Restructure monolithic CLAUDE.md (40k+ chars) into modular architecture: lean front page + CLAUDE/ topic files + docs/ for user content. All CLAUDE/ topic files created and @ pointers in place.

- [033-ddev-installation](033-ddev-installation/) - Install DDEV on rootful Docker (Approach C); rootless Podman remains default engine, LXC unchanged. End-to-end host run verified (`ddev v1.25.1` + `docker 29.4.0`).

- [00043-ipu6-webcam-fallout](00043-ipu6-webcam-fallout/) - Incident and recovery: the IPU6 play pulled an akmod that dragged in a half-installed kernel with no iwlwifi or btusb. Play rewritten to drop the akmod; recovery via the new `play-AB-dnf-upgrade.yml`, which also cleans up future half-installs.

- [00044-laptop-health-audit](00044-laptop-health-audit/) - Read-only audit of the daily-driver X1 Carbon, cross-checked against IaC to separate real gaps from busywork: five new or extended plays shipped, three items dropped. Established the "work WITH GNOME, not against it" principle.
