# Audit Coverage Gaps (Completeness Critic)

Areas the 10-dimension audit did not cover, identified and spot-checked by a dedicated completeness-critic agent. Each is a candidate for follow-up research.

## GAP-01: fedora-install/ bootstrap entirely unaudited (2,770 lines of security-critical install code)

Zero findings reference fedora-install/ despite it handling the highest-trust phase of the system lifecycle. Spot-checks found concrete issues: push.bash uploads environment/localhost/host_vars/localhost.yml (PII + vault blobs) to a GitHub repo '<user>/fedora-desktop-config' but ensure_config_repo() only checks the repo EXISTS (gh repo view), never that it is private — if a public repo with that name already exists, personal config is pushed publicly with no warning. ks.cfg (668 lines of embedded %pre/%post bash, not covered by qa-bash since it is not a .bash file) persists the WiFi PSK in plaintext to a NetworkManager connection file, copies vault-pass.secret into the user home, and uses an explicitly non-fatal repo pre-clone ('wizard handles failures' — soft-fail vs fail-fast rule). The whole directory deserves the same security/fail-fast/bash treatment the rest of the repo received.

Files: /workspace/fedora-install/push.bash, /workspace/fedora-install/ks.cfg, /workspace/fedora-install/setup-netinstall-boot.bash, /workspace/fedora-install/pull-projects.bash, /workspace/fedora-install/build-iso.bash

## GAP-02: run.bash automated failure-report flow posts hostname and weakly sanitised logs to the PUBLIC repo's issues

run.bash (1,470 lines, the primary entry point) appears in only one finding (OPP-09, a stale path). Its issue-reporting flow builds an issue body containing the machine's **hostname** (explicitly listed under 'Never Commit' in SecurityRules.md) plus a user-pasted error log whose sanitisation degrades silently: if the Claude-based sanitisation fails (`2>/dev/null || echo ""` at line 404 — itself an error-suppression pattern), it warns and proceeds with regex-only sanitisation before posting to the public LongTermSupport/fedora-desktop issue tracker. This is exactly the gh-CLI external-posting leak class that git hooks do not cover. The rest of run.bash (vault bootstrap, gh token scope checks, config-repo download at lines 839-885) also had no dedicated audit pass.

Files: /workspace/run.bash

## GAP-03: .gitignore correctness never audited — confirmed malformed line breaks the !.env.dist negation

Line 49 of .gitignore is literally `!.env.dist# Security incident documentation - DO NOT COMMIT` — a missing newline merged the negation pattern with the next section's comment header. In gitignore syntax a mid-line '#' is literal, so the pattern matches nothing: .env.dist files remain ignored (contrary to intent) and the security-incident section lost its header. Additionally, the public .gitignore advertises past-incident artefact names (SECURITY-INCIDENT-LOG.md, FORCE-PUSH-REQUIRED.md, '*-incident*.md'), a minor information leak. No SEC/QA/OPP finding assesses .gitignore content, even though OPP-01/QA-03 flagged tracked junk files that a correct ignore strategy relates to.

Files: /workspace/.gitignore

## GAP-04: Tracked .claude/ project-level custom code unaudited (42 tracked files incl. two Python hook handlers and a second CCY Dockerfile)

The exclusion list covered only .claude/hooks-daemon/ (upstream), but the repo tracks 42 files under .claude/ that are project-owned code: custom pre_tool_use handlers ansible_enforcement.py (248 lines) and system_paths.py (185 lines) with their tests, agent definitions, settings.json hook wiring, and .claude/ccy/Dockerfile — a second, distinct Dockerfile (extends claude-yolo with ansible/pipx) separate from files/var/local/claude-yolo/Dockerfile that the CCY dimension audited. None of the BSH/QA/CCY findings examine this code; QA-09 only notes the pytest suites are never executed. The handlers gate every Bash/Write call in sessions, so a logic bug there silently disables a safety control.

Files: /workspace/.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py, /workspace/.claude/hooks/handlers/pre_tool_use/system_paths.py, /workspace/.claude/ccy/Dockerfile, /workspace/.claude/settings.json

## GAP-05: Galaxy dependency supply chain: requirements.yml pins the vault-scripts role to 'master' and collections are unversioned

requirements.yml fetches LongTermSupport/ansible-role-vault-scripts at `version: master` and community.general / ansible.posix with no version at all; roles/vendor/* is gitignored, so despite the 'vendored roles' label nothing is actually vendored — every fresh install pulls whatever master currently is. That role handles vault-password operations, making it a meaningful supply-chain trust point. SEC-05 covered unpinned curl|bash installers but no finding covers Ansible Galaxy/git dependency pinning or integrity. A follow-up should pin to a tag/SHA or genuinely vendor the role.

Files: /workspace/requirements.yml, /workspace/.gitignore, /workspace/ansible.cfg

## GAP-06: Licensing compliance unexamined: GPL-2.0 script redistributed inside an MIT-licensed repo

Minor but unrepresented risk class. The repo LICENSE is MIT, yet files/usr/bin/gnome-shell-extension-installer is third-party code 'Licensed under the GNU General Public License 2.0' (header intact). BSH-19 flagged its quoting bugs but nobody assessed licence compatibility/attribution. The repo-level LICENSE implicitly claims MIT for all contents; a short third-party-licences note (or fetching the installer at deploy time instead of tracking it) would resolve it. Worth a quick sweep for any other bundled third-party files while at it.

Files: /workspace/LICENSE, /workspace/files/usr/bin/gnome-shell-extension-installer

