# Independent search: NOPASSWD sudoers rules whose target the granted user can steer

Searcher note: the dispatch described the class and hazard only; it did not contain the rule or its pattern. The detector files (src-go/qa/ansible/sudoers*.go, tests/fixtures/ansible-sudoers/) and files whose names mark them as secrets were not opened. The detector was not run.

## Forms expected before searching
- sudoers drop-ins (files/etc/sudoers.d), templates/copy/blockinfile/lineinfile with a sudoers dest, shell/printf/tee writes to /etc/sudoers*, kickstart %post, Go/Python writers, docs telling a user to add a rule.
- Hazard shapes: target file or any parent dir user-writable; command with no argument list; wildcards or unanchored regex; target that runs a PATH-looked-up program, a user-owned file or script, or honours env (BASH_ENV, LD_*, PATH, its own override variables); SETENV, env_keep, !env_reset, !secure_path; ALL grants; sudoedit; polkit analogues.

## Searches run
- git ls-files (minus excluded paths) piped to grep -il 'sudoers|NOPASSWD' (about 110 files, mostly plan/journal prose; every non-plan hit was read).
- grep for env_keep|SETENV|NOSETENV|secure_path|env_reset|Defaults|visudo|/etc/sudoers|doas|pkexec|polkit addRule across non-plan files.
- grep for the destinations /etc/sudoers.d and /etc/sudoers in playbooks, fedora-install, docs, scripts, src-go, helpers.
- grep for shutdown-with-update across the repo.
- Reading: both sudoers.d templates, the three scripts they point at, their deploy tasks and installer, play-basic-configs.yml, the kickstarts, docs.

## Inventory of every rule the repo installs, writes or tells a user to write

### 1. files/etc/sudoers.d/agent-bus-read-only.j2:9-10 (deployed by playbooks/imports/play-agent-bus.yml:258-265)
Verdict: near-instance WITHOUT the hazard (narrow, argument-pinned rule on a root script; the only rule in the repo with an argument regex).
- Granted to the desktop user (user_login), runas root, NOPASSWD. Commands: /usr/local/bin/agent-bus with args matching ^list [a-z][a-z0-9-]*$ and ^human devices <team> <name>$. The regexes are anchored at both ends, with character classes that exclude whitespace, slash and glob characters. They need sudo 1.9.10 or later, which the file's comment states.
- No Defaults line, no SETENV, no env_keep anywhere in the repo.
- Target: template to /etc/sudoers.d, root:root 0440, validate visudo -cf. The target script is installed by files/usr/local/sbin/agent-bus-install:499 as 0755 root:root into /usr/local/bin. LIB_DIR /usr/local/lib/agent-bus is ensure_dir 0755 root (agent-bus-install:490-492); the zipapp it runs sits there.
- What root does with the allowed arguments: files/usr/local/bin/agent-bus:155-169. Only seat and health use the caller's identity, and the script refuses both when EUID is 0, so they are unreachable through the rule. list and human go to run_admin_tool: cd /, then runuser -u agent-bus running /usr/bin/python3 -I on the root-owned pyz. Absolute interpreter and -I, so no PYTHON* and no cwd import. The tool runs as agent-bus, not root. The root-side file-placing paths (add-member, rotate-token, with --out) are not reachable through the pinned regexes.
- PATH lookups remain (runuser, mktemp, setpriv, tar, rm) but run under sudo's env_reset and secure_path. Nothing in the repo disables either.
- Residual (reading): the only thing left to steer is the agent-bus user's data, which is not the granted user's.
- Found by: text search (sudoers/NOPASSWD) located it; reading judged it.

### 2. files/etc/sudoers.d/fedora-desktop-self-update.j2:6-9 (deployed by playbooks/imports/optional/common/play-self-update.yml:401-409)
Verdict: near-instance WITHOUT the hazard (exact argument lists, hardened target).
- Granted to the desktop user, runas root, NOPASSWD. Four exact commands: run, run --dry-run, verify, status on /usr/local/sbin/fedora-desktop-self-update. No wildcard; sudo matches an exact argument list.
- Template root:root 0440 with visudo validation. The target is copied by play-self-update.yml:317 (root-owned, the script header says 0700, in /usr/local/sbin). It fixes PATH itself (script line 40), uses constant paths, and checks signatures on its deploy clone before importing any code (script 85-149). The clone is asserted to be outside /home and root-owned and user-unwritable throughout (play-self-update.yml:260-270, 469-503).
- One test-only env override exists (FEDORA_DESKTOP_SELF_UPDATE_TEST_PREFIX, script line 39). The script comment says sudo's env_reset drops it and the script is 0700. It is safe only because no SETENV or env_keep exists; the repo has no Defaults line anywhere, so it holds. Same pattern in helpers/self_update/cycle.py:91 (a countdown override).
- Found by: text search; reading judged the fixed PATH, the signing gate and the env override.

### 3. files/usr/local/bin/shutdown-with-update, candidate (no sudoers rule exists)
Verdict: no rule yet, so nothing in the class today. Current access is the alias files/home/bashrc-includes/shutdown-with-update.bash:5-6 (sudo with the full path), which asks for a password. Deployed by playbooks/imports/play-basic-configs.yml:360-366 as root:root 0755 into /usr/local/bin; reboot-with-update is a root-owned symlink (371-377). The profile file is root:root 0644 in /etc/fedora-desktop, a 0755 root directory (335-358). File and directory ownership would not fail a rule on it. What a rule on it would depend on:
- Environment (the hazard if SETENV, env_keep or !env_reset were ever added): lines 104-106 read WITH_UPDATE_MINUTE_SECONDS, WITH_UPDATE_SESSIONS_TIMEOUT_SECONDS and WITH_UPDATE_PROFILE_FILE from the environment, commented "nothing else should ever set them". Lines 366 and 370 feed MINUTE_SECONDS into bash arithmetic and into the countdown wait. Bash arithmetic evaluates a value such as a[$(cmd)] as code, so a caller-supplied value is root code execution. Line 106 lets the caller choose the profile file. Sudo's default env_reset drops all three today, which is what the self-update script relies on; unlike that script, this one has no comment saying so and no fixed-environment guard, so a rule would rest wholly on the absence of any Defaults line. The hazard exists only under SETENV/env_keep/!env_reset (confidence high); it does not exist today.
- PATH: none is set. Root calls the firmware tool, the OS updater, the app-store updater, logger, timeout, getent, cut, the process lister, the service manager, the power-off command, awk and the wait command by bare name. Safe under sudo's default secure_path; steerable if secure_path were removed or the caller's PATH kept.
- Arguments: the parser (158-197) accepts only -h/--help, --strict-sessions and --in with a positive-integer regex; anything else exits 64. A rule with no argument list would be safe on arguments, because nothing from the caller reaches a root command except the validated minute count.
- User-controlled files: root runs no user-owned file as root. It runs the user's ccy-sessions and the user's login shell (bash -lc for pipx and rustup) only through the sudo user-switch to SUDO_USER, that is as the user, so no extra privilege. user_home comes from getent, not the environment. SUDO_USER is set by sudo, and the script refuses root and unset.
- Root actions on user-supplied state: the system-wide updates run as root with whatever HOME and environment sudo leaves; HOME handling depends on the host's sudo defaults, which I did not verify. They read root-configured sources.
- What decides the hazard for a rule on it: (a) no Defaults granting SETENV or env_keep for it, (b) target and parents root-owned and not group/world-writable, (c) the env-read variables not honoured under the host's sudo env handling. It would be the second root target in the repo that reads environment overrides, and the first that does arithmetic on one.
- Found by: reading the script; text search (grep for the script name) found the deployment and the alias.

### 4. playbooks/imports/play-basic-configs.yml:127-133
blockinfile into /etc/sudoers: the user, ALL=(ALL) NOPASSWD: ALL, validate visudo -cf. Developer desktop only; removed for the server profile and a consumer install (state absent).
Verdict: not in class. It is a deliberate full grant, so there is no narrow action to subvert and no target to steer. Found by: text search.

### 5. fedora-install/ks.cfg:500-501
echo of the kickstart user with ALL=(ALL) NOPASSWD: ALL into /etc/sudoers.d/99-fedora-desktop-setup, mode 440. Verdict: not in class (full grant). Found by: text search.

### 6. fedora-install/ks-vm-desktop.cfg:80-81 and fedora-install/ks-vm-server.cfg:50-51
printf of the lab user with ALL=(ALL) NOPASSWD: ALL into /etc/sudoers.d/90-vmtest-lab, mode 0440. VM lab user only. Verdict: not in class (full grant, throwaway lab VM). Found by: text search.

### 7. docs/headless-server-install.md:73-74
Tells the operator to write a NOPASSWD:ALL line to /etc/sudoers.d/90-<user>-nopasswd, mode 0440. Verdict: not in class (full grant; a documentation instruction, not an installed rule). Found by: text search.

## Near-instances outside sudoers and non-rules
- files/etc/polkit-1/rules.d/50-displaylink-dock-recovery.rules.j2 (deployed by playbooks/imports/optional/hardware-specific/play-displaylink.yml:377-378): polkit rule letting the desktop user start exactly displaylink-dock-recovery.service from a local active session without a password. Not sudoers, so not in class, but the same shape: a narrow grant whose target runs as root, and the file's own comment says the desktop user can set off what the recovery does. I did not read the unit or its script, so I cannot say whether it reads user-writable state. Verdict: near-instance, unverified.
- src-go/internal/sudo/sudo.go:19-20, src-go/internal/agentbus/agentbus.go:18, helpers/agent_bus/checkout.py:908-914, docs/agent-bus.md:211,615, docs/playbooks.md:792,1513, src-go/internal/teams/usage.txt:75: consumers and descriptions of rules 1 and 2. Not rules.
- run.bash (e.g. 1072) and docs/headless-provisioning.md:44: say a command-scoped rule is unsupported as a run credential. Prose.
- scripts/test-ccy-teams-command.bash:474-488: parses rule 1's template in a test; writes nothing to /etc.
- files/home/.local/share/vmtest/guest-acceptance-desktop-consumer-install.bash:121-129,196 and src-go/internal/vmtest/run.go:130: checks that read /etc/sudoers; write nothing.
- No other file writes a sudoers rule: no Python or Go writer, no lineinfile, no tee outside the kickstart and doc sites above. No Defaults, SETENV, env_keep, !env_reset or !secure_path appears in any non-plan file. No sudoedit rule. Plan and journal files discuss rules (00063, 00137, 00177, 00201, 00209 and others) but were not treated as installers and were not read individually.

## Count
- Narrow NOPASSWD rules installed: 2 (neither carries the hazard).
- Full NOPASSWD:ALL sites: 5 (1 playbook, 3 kickstart files, 1 doc).
- Candidate target without a rule: 1 (shutdown-with-update). Conditional hazard through env, not present today.
- No instance carries the hazard today.

## Attribution by technique
Text search only found: the kickstart writes, the doc instruction at docs/headless-server-install.md:73, the blockinfile in play-basic-configs.yml, and the /etc/sudoers readers in the guest-acceptance script and the tests. They sit in files unrelated to the two drop-ins.
Reading only found: that the shutdown-with-update env-read variables (104-106) reach bash arithmetic at 366 and 370 (steerable by env); that agent-bus seat/health are unreachable through rule 1 because the wrapper refuses EUID 0; that the self-update script's env override depends on there being no Defaults line; the polkit analogue; the deployment owner/mode chain (agent-bus-install ensure_dir/put_file, the play tasks) that no grep for sudoers connects to the rules.
Neither technique can check: the sudo defaults (env_keep, secure_path, HOME handling) and actual file modes on a deployed host; the repo ships no Defaults, and /etc/sudoers on a real machine is outside it.
