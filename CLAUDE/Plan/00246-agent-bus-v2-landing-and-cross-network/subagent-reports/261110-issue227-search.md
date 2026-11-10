# Issue 227 independent search: Class A (record guard) and Class B (membership filter)

Scope: playbooks/, tasks/, vars/, environment/ (tracked yml), exclusions as briefed. The dispatch did not contain the rule or its pattern. src-go/qa/ and detector configs were not opened; no detector was run. The agent had no Write tool, so this file was written through the shell.

## Counts

- Class A: 2 firm instances (1 known, 1 new), 3 likely/variant, 4 unsure variants; close calls listed.
- Class B: 2 firm instances (1 known, 1 new); the rest judged not instances.

## Class A: install guard trusts a record without the artefact

### Firm

1. playbooks/imports/play-teamchat.yml:336-342 (known). Stat: `tuwunel-<ver>-asset.sha256` under tc_share_dir (338), compared with the pin hash (342). Guarded block (342-376) downloads, unpacks, installs `tc_server_dir/tuwunel-<ver>` (368), then writes the record (376). The binary is never stat'ed. Binary deleted, record intact: block skipped, play succeeds, binary not restored. Technique: text search (stat of an `-asset.sha256` path) and reading.

2. playbooks/imports/optional/common/play-self-update.yml:704-745 (new). Stat/slurp of the collections stamp `self_update_collections_stamp`, compared with the sha256 of the deploy clone's requirements.yml (`when` and `changed_when`, about 736-741). Guarded: "Install the Collections into the Root-Owned Path" (galaxy install into `self_update_collections_dir`); the stamp is rewritten by "Record the Installed Requirements Hash" right after. The installed collections directory is not checked before the install. Collections deleted or damaged, stamp intact: install skipped, play succeeds. The later find task checks ownership and write bits only, not completeness. Technique: text search (`stamp`) and reading. Confident.

### Likely / variant

3. playbooks/imports/play-rpm-fusion.yml:26-35. `creates:` names the marker `/var/local/.rpmfusion-multimedia-configured`, written by `touch` as the shell's last line. Guarded unit sets the openh264 repo option and installs the multimedia package group. Packages not checked. Group removed, marker kept: never reinstalled. The play's comment says the guard skips the whole block after first run. Technique: text search (`creates:`). Confident.

4. playbooks/imports/play-toolbox-install.yml:33-35, 115. Stat is of the data directory `~/.local/share/JetBrains/Toolbox`; the guarded block (download, `toolbox --install`) is skipped when it exists. The binary lives below that directory and is not stat'ed. Binary deleted, directory kept: no reinstall. The launch task (about 139-156) fails only when the symlink or desktop entry is also missing; otherwise the run succeeds silently. Technique: text search for stat guards, then reading to see where the binary sits. Moderately confident.

5. playbooks/imports/optional/common/play-ddev.yml:107-112. `mkcert -install` guarded by `creates: ~/.local/share/mkcert/rootCA.pem`. The command also installs the CA into the system/NSS trust stores; only the CA file is named. Trust entry removed, CA file kept: not re-run. The assert after it only checks the CA file is non-empty. Technique: text search and reading. Unsure whether trust-store state counts as an artefact.

### Unsure (guard names one of several files the unit writes, or a directory not its content)

6. play-lxc-install-config.yml:208-213 (ssh-keygen, `creates: ~/.ssh/id_lxc`; `.pub` not named). Same shape: optional/common/play-vm-test-lab.yml:280-293 (`lab-key`). The private key is asserted non-empty; the public half is derivable.
7. optional/hardware-specific/play-nvidia.yml:229-232 `kmodgenca -a`, `creates: public_key.der` (private key not named).
8. play-fonts.yml:40-45 msttcore installer rpm, `creates: /usr/share/fonts/msttcore` (directory; the installer fetches the fonts into it). Fonts gone, directory kept: not redone.
9. optional/hardware-specific/play-nvidia.yml:185-190 cuDNN unarchive, `creates: /opt/cudnn/nvidia/cudnn/lib` (directory, not libraries).

## Class A close calls judged NOT instances

- play-basic-configs.yml:593-600 yq: `creates: /usr/bin/yq` is the artefact itself. Existence-only (truncated or v3 file kept), but a later probe and assert surface it loudly.
- Artefact-itself or artefact-root `creates:`: play-nvm-install.yml:34 (nvm.sh), :82 (versions/node dir); play-python.yml:280 (~/.pyenv dir); play-rust-dev.yml:53, 119; play-qobuz.yml:158; play-cli-tools.yml:311; play-network-tools.yml:76; tasks/install-fzf.yml:62; play-vscode.yml:44; play-musiccast.yml:123; play-gnome-shell-extensions.yml:108; play-vm-test-lab.yml:547; play-virtualbox-windows-vm-setup.yml:88, 118.
- Repo-file guards (the unit writes exactly that file): play-browsers.yml:217, 239; play-docker.yml:99; play-terminal-emulators.yml:54.
- Probe the installed thing: play-photography.yml ART (247-260, stat of the AppImage get_url writes) and RapidRAW (171-195, package query); play-python.yml:135-165 (`--version` of pipx tools); play-markless.yml:38-45 and play-qobuz.yml:63-76 (version read from the binary); play-claude-yolo.yml:285-303 (stat of installed claude).
- play-podman.yml:146-150, 214 (`container-images.succeeded` stamp): gates running a prune, not an install.
- play-agent-bus.yml:135-171 compares checksums of the installed files themselves. play-teamchat.yml:255-270 verifies copied artefacts by hash.
- Installer scripts under files/ (for example agent-bus-install): not Ansible sources, not read.

## Class B: list filtered by membership in another list, no check members exist

### Instances

1. playbooks/imports/play-teamchat.yml:420 (known). L = `teamchat_approve_teams` (operator input, host_vars). S = `tc_scoped_present` (`tc_present` filtered by `teamchat_only_teams`, line 74). Subset check: assert at line 96 is against `tc_present`, not the scoped list. A team in approve but outside only_teams passes the assert and is dropped silently. Technique: text search (selectattr with `'in'`).

2. playbooks/imports/play-teamchat.yml:74-75 (new). L = `teamchat_only_teams` (operator input). S = `tc_present` / `tc_absent`. The only check on L (line 94) is its type (sequence, not string); members are not checked against declared teams. A typo is dropped silently; with only a typo given, nothing is applied and the play reports success. Technique: text search, confirmed by reading the assert block. Confident.

### Checked, covered

- play-agent-bus.yml:63-66 (`agent_bus_only_teams` over `agent_bus_present`/`agent_bus_absent`): operator input; assert at 104-105 requires `only_teams | difference(all declared teams)` empty. The union of the S lists is covered, so every name matches one. Not an instance.
- play-agent-bus.yml:77-78 (`agent_bus_only_teams` over `agent_bus_lxc_members`): L validated at 104-105 against declared teams; a team without LXC members is by design.

### Not instances

- optional/common/play-agent-bus-element.yml:37 `rejectattr('team','in', agent_bus_element_absent)`: L derived from agent_bus_teams entries marked absent; an absent team with no profile entry is normal.
- play-agent-bus.yml:168-172: both lists come from one loop result.
- tasks/podman-store-busy-guard.yml:56: both lists derived from probes; a lock with no holder is by design.
- optional/common/play-speech-to-text.yml:771-774: `intersect` as a deny-check (must be empty), not a selection.
- play-python.yml:51; play-AB-dnf-upgrade.yml:170-182: set arithmetic over probe output.
- Architecture membership asserts and scalar `in` tests (play-lastpass.yml etc.).
- No `when: item.x in <list var>` loop over an operator list and no Jinja for-loop-with-if-in filter found.

## Technique attribution

Text search found, and reading could not have improved on:
- Every `creates:` (about 55 hits) and every stat `path:` (about 100); these produced the rpm-fusion marker, the self-update stamp, the toolbox directory, the teamchat record.
- Every selectattr/rejectattr with `'in'`, `intersect`, `difference`: the full Class B candidate set, including the unvalidated `teamchat_only_teams`.

Reading found, and text search could not have:
- That self-update's slurp plus checksum compare in `when:` is a record guard (no `creates:`, no marker-like name on the guard).
- That the toolbox stat names the data directory rather than the binary (needed to see where `--install` puts it).
- That the teamchat approve assert (96) is against `tc_present` rather than the scoped list, that line 94 omits a membership check, and that agent-bus 104-105 has the check teamchat lacks.
- That ddev, ssh-keygen and kmodgenca guards name one file of several written.
- Which `creates:` hits are artefact-itself guards (the close-call judgements).

## Searches run

- `creates:|removes:`; every `stat:` and its `path:`; `.stat.exists` and its negations.
- Path words: version, `.sha256`, marker, stamp, installed, done, record, pin, checksum, lock, `-asset`.
- Shell-level guards: `[ -f`, `[[ -f`, `test -`, `command -v`, "already installed".
- Class B: selectattr, rejectattr, select, reject, intersect, difference, union, symmetric, `'in'`, `"in"`, `when:` lines and multi-line `that:`/`when:` with ` in `, `item ... in`, Jinja for-if-in loops, `_only_|_approve|only_teams`.
- Read around each hit: play-teamchat, play-agent-bus, play-self-update, play-toolbox-install, play-ddev, play-rpm-fusion, play-basic-configs (yq), play-photography, play-markless, play-qobuz, play-nvidia, play-lxc-install-config, play-vm-test-lab, play-fonts, play-python, play-podman, play-claude-yolo, tasks/*.yml.

Saturation: the final searches (shell-level guards, `.stat.exists` forms, multi-line `when:`) turned up nothing new.
