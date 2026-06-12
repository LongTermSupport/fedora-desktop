# Performance Audit

## Scope & Method

Audited repository `/workspace` (fedora-desktop, branch F43) for performance and efficiency issues across:

- `playbooks/` — all core plays imported by `playbook-main.yml` read in full or sweep-grepped for unguarded `shell:`/`command:` tasks; optional plays sampled where shell-task density was high.
- `scripts/` — QA suite executed and timed (`time ./scripts/qa-all.bash`), sub-scripts timed individually, shellcheck pass isolated and re-timed with alternative file scoping. Git hooks read in full.
- `files/var/local/claude-yolo/` — CCY Dockerfile read in full and analysed for layer-cache behaviour; `claude-yolo` wrapper skimmed for per-launch hot-path costs.
- `extensions/` — all three extensions checked for polling vs event-driven patterns.
- Git repository size: `git count-objects -vH` and per-file `du` over tracked files.

Excluded as instructed: `.git/`, `node_modules/`, `untracked/`, `.claude/hooks-daemon/` (audited only as a *target* of repo QA scans, not its contents), `roles/vendor` (same).

All timings below were measured in this session on this machine; absolute values will differ on other hardware but the ratios hold.

## Summary

The repository is small (`.git` = 6.9 MB, 409 tracked files) and most playbooks are well-guarded with `creates:`, `changed_when:` and stat-gated blocks. The two largest concrete wins found:

1. **QA suite spends ~65% of its wall-clock scanning code the project does not own.** `qa-all.bash` took **44.9 s**; 38.3 s of that is `qa-bash.bash`, which scans 196 bash files — but ~91 are under `.claude/` (mostly the upstream hooks-daemon) and 23 under `roles/` (vendored). Scoping the shellcheck pass to project files alone drops it from 23.6 s to 8.3 s, and 230 of the 310 reported shellcheck diagnostics are vendor noise. Since CLAUDE.md mandates this script before *every* commit, the saving compounds heavily.
2. **The CCY Dockerfile busts its own layer cache.** `ARG DOCKERFILE_HASH` (the md5 of the Dockerfile itself) is consumed by a `LABEL` at line 32, before every expensive layer. Any one-line edit anywhere in the Dockerfile changes the hash, invalidates the LABEL layer, and forces a full rebuild of all four apt transactions, the Claude Code npm install, the Chromium dependency set and agent-browser download — typically many minutes — even when the edit only touched a doc `COPY` at the bottom.

Beyond those, several core playbooks repeat expensive network/dnf work on every `playbook-main.yml` run: `play-rpm-fusion.yml` runs seven unguarded dnf transactions (including `dnf -y update @core`), `play-basic-configs.yml` forces a fwupd metadata re-download (`refresh --force`) every run, and `play-nvm-install.yml` recursively chowns the entire `~/.nvm` tree (tens of thousands of files once node versions are installed) every run.

## PERF-01: qa-all.bash spends ~30 s scanning upstream and vendored code

**Severity: medium · Area: scripts · Effort: small**

### Evidence

Measured on this machine:

| Run                                                           | Wall clock |
| ------------------------------------------------------------- | ---------- |
| `./scripts/qa-all.bash` (full)                                 | **44.9 s** |
| `qa-bash.bash` alone                                           | 38.3 s     |
| `qa-patterns.bash` (semgrep) alone                             | 3.5 s      |
| shellcheck over the 150 discovered `*.sh`/`*.bash` files       | 23.6 s     |
| shellcheck over the same set minus `.claude/*` (59 files)      | **8.3 s**  |

File discovery in `scripts/qa-bash.bash:25-47` excludes `.git`, `node_modules`, `untracked`, `.ansible/roles`, and two `.claude/ccy` subdirs — but **not** `.claude/hooks-daemon/` (the upstream dependency CLAUDE.md says must remain untouched) and **not** `roles/vendor/` (the pattern `*/.ansible/roles/*` does not match the repo path `roles/vendor`). Breakdown of the 150 discovered named bash files by top-level directory:

```
 91 .claude   (50 hooks-daemon/scripts, 15 hooks-daemon/CLAUDE, 17 ccy/plugins, …)
 23 roles     (vendored)
 15 files
 13 scripts
  4 fedora-install
  …
```

Of the 310 shellcheck diagnostics surfaced by every QA run, attribution by source:

```
230  roles/vendor          ← vendored, not fixable here
 18  .claude/hooks-daemon  ← upstream, must not be edited
 11  .claude/hooks
 …
```

So ~80% of the diagnostic output is permanent noise against code the project is forbidden from fixing, and ~65% of the shellcheck wall-clock is spent producing it. Note the inconsistency: `scripts/qa-python.bash:33` *does* exclude `*/.claude/hooks-daemon/*`, confirming the intent; `qa-bash.bash` simply lacks the same exclusions. `qa-patterns.bash:41-47` likewise excludes `.ansible/roles` but not `roles/vendor` or `.claude/hooks-daemon`.

Two secondary inefficiencies in `qa-bash.bash`:

- The second discovery loop (`scripts/qa-bash.bash:34-47`) runs `head -n1` on **every executable file** in the repo, including binaries — visible as the repeated `warning: command substitution: ignored null byte in input` on every QA run (line 35).
- The syntax-check loop (`scripts/qa-bash.bash:52-62`) spawns `bash -n` **plus** a `jq -nc` per file (~400 process spawns for 196 files), accounting for most of the non-shellcheck remainder (~14 s).

### Impact

CLAUDE.md mandates `./scripts/qa-all.bash` before *every* commit touching bash/Python/Ansible. At 45 s per run vs an achievable ~15 s, every commit pays a ~30 s tax, and the 310-issue shellcheck banner trains everyone to ignore the diagnostics channel entirely.

### Recommendation

Add `! -path "*/.claude/hooks-daemon/*"` and `! -path "*/roles/vendor/*"` (or better, `! -path "*/.claude/*"` plus `roles/vendor`) to both `find` invocations in `qa-bash.bash`, mirroring `qa-python.bash`; add the same `--exclude` entries to `qa-patterns.bash`. Alternatively scope discovery to `git ls-files`, which excludes everything untracked/vendored by construction. Optionally batch the `bash -n` results into a single `jq` invocation at the end instead of one per file.

## PERF-02: CCY Dockerfile layer cache is busted by any edit (hash LABEL at top)

**Severity: medium · Area: ccy · Effort: small**

### Evidence

`files/var/local/claude-yolo/Dockerfile`:

```dockerfile
22  ARG DOCKERFILE_HASH="unknown"
...
31  LABEL claude-yolo-version="2.18"
32  LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"
```

`DOCKERFILE_HASH` is the md5 of the Dockerfile itself, computed by `playbooks/imports/play-claude-yolo.yml:332-335` (and equivalently by the `claude-yolo` wrapper's rebuild path). Because the ARG is consumed in a `LABEL` at line 32 — *before* every expensive layer — **any** edit to the Dockerfile (a comment, a doc `COPY` at line 181, the final LSP stage) changes the hash, invalidates the line-32 layer, and cascades a rebuild of everything after it:

- 4 separate `apt-get update && install` transactions (lines 43-68, 80-91, 94-100, 126-145)
- `pipx install ansible` + semgrep (lines 108-113)
- `npm install -g @anthropic-ai/claude-code` (line 123)
- `npm install -g agent-browser && agent-browser install` — a Chromium download (lines 148-149)

Only the independent `phpantom-builder` stage (lines 7-16) survives. In practice this means a one-line docs change to the image costs the full multi-minute rebuild, both on the host (`play-claude-yolo.yml` build task at lines 337-344) and via the wrapper's hash-mismatch auto-rebuild.

### Impact

Every Dockerfile iteration during CCY development pays a full-image rebuild (~5-15 min, plus several hundred MB of downloads) instead of the seconds a cached rebuild would take. The repo's own ctrl+z-patch maintenance workflow (documented in CLAUDE/ContainerRules.md) requires exactly this kind of small edit.

### Recommendation

Move both `LABEL` lines (and the `ARG DOCKERFILE_HASH` consumption) to the **end** of the final `full` stage (after line 231). OCI labels are image-level metadata — the wrapper's `image inspect` validation keeps working regardless of which layer sets them — but as the last layer they no longer invalidate anything else. Keep the `ARG` declaration where it is; only the consuming instruction matters for cache keys.

## PERF-03: play-rpm-fusion.yml runs seven unguarded dnf transactions on every main run

**Severity: medium · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/play-rpm-fusion.yml:6-18` — the play's only task, imported unconditionally by `playbook-main.yml:17`:

```yaml
- name: Install RPM Fusion
  become: true
  ansible.builtin.shell: |
    set -x
    dnf -y install https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm
    dnf -y install https://download1.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm
    dnf -y config-manager --enable fedora-cisco-openh264
    dnf -y update @core
    dnf -y group install multimedia --allowerasing
    dnf -y update @multimedia --setopt="install_weak_deps=False" --exclude=PackageKit-gstreamer-plugin
    dnf -y install intel-media-driver
```

No `creates:`, no `changed_when:`, no conditional. Every `playbook-main.yml` run therefore performs: two remote-URL rpm installs (each fetching the rpm headers from rpmfusion.org), two `dnf update` depsolves and two group operations — each a full dnf transaction with metadata checks — even though `play-AB-dnf-upgrade.yml` has already brought every package to latest minutes earlier in the same run. The task also always reports `changed`.

### Impact

Several no-op dnf transactions and remote fetches per run; on a typical run this is one of the longest "did nothing" stretches in `playbook-main.yml`. Contrast with `play-vscode.yml:20` and `play-browsers.yml:64,85`, which correctly guard equivalent shell blocks with `creates:`.

### Recommendation

Split into idempotent module tasks: `ansible.builtin.dnf` with the two release rpms as `name:` (the dnf module handles remote URLs and is a no-op when installed), a `creates: /etc/yum.repos.d/rpmfusion-free.repo`-style guard for the config-manager step, and drop or gate the `update @core`/`@multimedia` lines (redundant with play-AB-dnf-upgrade). At minimum add `creates: /etc/yum.repos.d/rpmfusion-nonfree.repo` to skip the whole block after first install.

## PERF-04: fwupd firmware refresh forced on every main run

**Severity: medium · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/play-basic-configs.yml:240-257`:

```yaml
- name: Check and Apply Firmware Updates via fwupd
  ansible.builtin.shell: |
    set -e
    fwupdmgr get-devices
    fwupdmgr refresh --force
    ...
  changed_when: true  # get-updates/update return rc=2 when no updates available
```

`fwupdmgr refresh --force` bypasses fwupd's metadata-age cache and re-downloads LVFS metadata on **every** `playbook-main.yml` run; `get-devices` enumeration and the update probe add more time. The task is also hard-coded `changed_when: true`, so it can never report a clean no-op.

### Impact

Tens of seconds of network and device enumeration per run for an operation that fwupd itself rate-limits by design (plain `refresh` is a no-op when metadata is fresh). Firmware application mid-playbook is also a surprising side effect of a play named "Basic Configs".

### Recommendation

Drop `--force` (plain `fwupdmgr refresh` respects metadata age and exits quickly when fresh), and derive `changed_when` from whether `fwupdmgr update` actually applied anything (e.g. register output and match on update text) so idempotent runs report unchanged. Consider moving firmware updates to a tagged/optional play.

## PERF-05: Recursive chown of the entire ~/.nvm tree on every run

**Severity: medium · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/play-nvm-install.yml:31-37`:

```yaml
- name: Set NVM Directory Permissions
  ansible.builtin.file:
    path: "/home/{{ user_login }}/.nvm"
    owner: "{{ user_login }}"
    group: "{{ user_login }}"
    recurse: true
    state: directory
```

The play runs in `playbook-main.yml:11` on every run. With one or more node versions installed (plus per-version global `node_modules`), `~/.nvm` contains tens of thousands of files; `recurse: true` stats every one of them, every run. The ownership it enforces can only be wrong immediately after the install task above it (lines 22-29, correctly guarded with `creates:`) — never on subsequent runs, since everything under `~/.nvm` is created by the user's own nvm.

### Impact

A large recursive filesystem walk on every main run that is a guaranteed no-op after first install. Cost grows with every node version installed.

### Recommendation

Register the "Download and Install NVM" task result and run the recursive permission fix only `when:` that task reported changed (first install), or drop `recurse: true` entirely — the installer runs as the user already.

## PERF-06: play-python.yml performs PyPI network work on every run

**Severity: low · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/play-python.yml`:

- Lines 48-51, 55-58, 63-66: three `community.general.pipx` tasks with `state: latest` (pdm, huggingface_hub, semgrep) — each consults PyPI for the latest version on every run.
- Lines 53-54: `ansible.builtin.shell: pdm self update` — unguarded, no `changed_when`, always reports changed, hits the network every run, and is redundant immediately after `state: latest` has just upgraded pdm.
- Lines 87-93: the pyenv version loop sources `~/.bash_profile` and runs `pyenv install -s` per version with no `changed_when` — cheap, but three always-`changed` tasks per run.

### Impact

Four-plus network round-trips and several misleading `changed` statuses on every main-adjacent run. If "always latest" is a deliberate policy the network cost is inherent, but the `pdm self update` task is pure duplication.

### Recommendation

Delete the `PDM Self Update` task (covered by `state: latest`), and add `changed_when` based on output to the pyenv install loop (e.g. match on `Installed`). If run-time matters more than freshness, switch the pipx tasks to `state: present` and upgrade explicitly when wanted.

## PERF-07: Unguarded every-run network/system commands in core plays (flatpak, firewalld)

**Severity: low · Area: playbooks · Effort: small**

### Evidence

- `playbooks/imports/play-comms.yml:11-16`: `flatpak remote-add --if-not-exists …` and `flatpak install flathub com.slack.Slack -y` as raw `command` tasks — both run (and report `changed`) every run; the install contacts Flathub each time even when Slack is already installed.
- `playbooks/imports/play-lxc-install-config.yml:71-75`: `nmcli connection modify lxcbr0 connection.zone trusted` followed by `firewall-cmd --change-interface … --permanent && firewall-cmd --reload` — unguarded, so the firewall is fully reloaded on every main run even when nothing changed.

### Impact

A Flathub round-trip plus a firewalld reload (which briefly reprocesses all zones/rules) on every run, and three permanently-`changed` tasks polluting the recap.

### Recommendation

Use `community.general.flatpak_remote` and `community.general.flatpak` (both idempotent), and `ansible.posix.firewalld` with `immediate: true permanent: true` for the zone/interface binding (idempotent, no blanket reload). The nmcli step can use `community.general.nmcli`.

## PERF-08: play-rust-dev.yml repeats toolchain updates and duplicates a component task

**Severity: low · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/optional/common/play-rust-dev.yml`:

- Lines 67-75: `rustup update stable` + `rustup default stable` every run, no `changed_when` — network check and possible toolchain download on every run, always `changed`.
- Lines 77-90: `rustup component add {{ item }}` loop over five components including `rust-analyzer`; lines 153-162 then repeat `rustup component add rust-analyzer` as a separate task — duplicate work and a second always-`changed` shell.
- Lines 164-174: verification shell without `changed_when: false`.

### Impact

Optional play, so per-run cost is opt-in, but each invocation performs redundant network checks and reports five-plus spurious `changed` states; the duplicated rust-analyzer task is dead weight.

### Recommendation

Remove the duplicate rust-analyzer task (lines 153-162); add `changed_when` derived from rustup output (`"installing"`/`"updated"`) to the update and component tasks; add `changed_when: false` to the verify task.

## PERF-09: GNOME extension installer queries extensions.gnome.org seven times per run

**Severity: low · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/play-gnome-shell-extensions.yml:23-48` loops `gnome-shell-extension-installer --yes --restart-shell {{ item.id }}` over seven extension IDs with no `changed_when`/guard. Inspection of `files/usr/bin/gnome-shell-extension-installer` (lines 84-96) shows it does skip the download when the installed version is current, but it still issues an HTTP metadata request per extension per run to determine that, and each loop iteration always reports `changed`.

### Impact

Seven sequential HTTP round-trips to extensions.gnome.org on every `playbook-main.yml` run (the play is in the main import list, line 38), plus seven spurious `changed` results.

### Recommendation

Capture the installer's stdout and set `changed_when` on its "installed/upgraded" markers; optionally pre-check `~/.local/share/gnome-shell/extensions/<uuid>` existence and only invoke the installer for missing ones (accepting that version refreshes then need a tag or periodic run).

## PERF-10: Extensions use timer polling where event-driven APIs exist

**Severity: low · Area: extensions · Effort: medium**

### Evidence

- `extensions/remote-desktop-toggle@fedora-desktop/extension.js:20,64-71`: spawns the `rdt status` subprocess every 10 s (`REFRESH_INTERVAL_SECONDS = 10`) for the life of the session, purely to keep a quick-settings toggle's `checked` state fresh — a process spawn plus grdctl/D-Bus work every 10 s even when the menu is never opened.
- `extensions/speech-to-text@fedora-desktop/extension.js:509-533`: polls every 5 s, doing two `Gio.File.query_exists` stats on the wsi-stream pid/socket files to toggle a status dot.

Both timers are correctly cleaned up in `destroy()`/`disable()`; this is purely an efficiency observation.

### Impact

Continuous wakeups and (for rdt) subprocess spawns on battery-powered laptops — the exact hardware class this repo targets. Each individual poll is cheap; the cost is the permanent 10 s/5 s wakeup cadence in the shell process.

### Recommendation

For speech-to-text, replace the 5 s stat poll with a `Gio.FileMonitor` on `$XDG_RUNTIME_DIR` watching the pid/socket files (pure event-driven, zero idle cost). For remote-desktop-toggle, refresh on menu-open (`Main.panel.statusArea.quickSettings` menu `open-state-changed`) and after toggle actions instead of a fixed timer, or watch the underlying systemd unit via `Gio.DBus` `g-properties-changed`.

## PERF-11: wsi-stream server killed on every speech-to-text play run

**Severity: low · Area: playbooks · Effort: small**

### Evidence

`playbooks/imports/optional/common/play-speech-to-text.yml` (task at the `pkill` block, ~lines 269-276):

```yaml
- name: Stop wsi-stream-server if Running (to pick up updated scripts)
  ansible.builtin.shell:
    cmd: pkill -f wsi-stream-server
```

This runs unconditionally before the script `copy` tasks, so every play run kills the warm streaming server — which then has to re-load its speech model (the play itself warns model setup "CAN TAKE MANY MINUTES") — even when no script content changed.

### Impact

User-visible cold-start latency on the next dictation after every playbook run, including pure no-op runs.

### Recommendation

Convert the kill to a handler and `notify:` it from the `wsi-stream`/`wsi-stream-server` copy tasks, so the server is only restarted when a deployed script actually changed.

## PERF-12: Two large PDFs tracked in git (≈37% of pack size)

**Severity: info · Area: docs · Effort: small**

### Evidence

```
1 011 085  CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-basic.pdf
  800 048  CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-advanced.pdf
```

against a total pack size of 4.86 MiB (`git count-objects -vH`). The next-largest tracked file is the 123 KB `claude-yolo` script.

### Impact

Negligible today — the repo is small — but binary vendor PDFs in history are permanent (they cannot be removed without history rewriting, per the project's own SecurityRules guidance) and set a precedent for plan folders accumulating binary assets.

### Recommendation

For future plans, store vendor spec documents as links (or under `untracked/`) rather than committing binaries; no action needed for the existing files unless repo size becomes a concern.

## Positive Observations

- **Guarding discipline is generally good.** The majority of `shell:`/`command:` tasks across the 60+ playbooks carry `creates:`, `changed_when: false` probes, or stat-gated blocks: e.g. `play-nvm-install.yml` (creates-guarded installer and node install), `play-claude-code.yml` (stat-gated installer), `play-toolbox-install.yml` (the entire download/install block is skipped via a stat check, so the JetBrains releases API is not hit on subsequent runs), `play-vscode.yml` and `play-browsers.yml` (`creates:` on repo setup), and `play-markless.yml` (explicit version comparison).
- **Package installs are batched.** dnf/package tasks pass full lists in single transactions (`play-python.yml`, `play-basic-configs.yml`, `play-rust-dev.yml`); no per-package loops were found anywhere (`play-AB-dnf-upgrade.yml`'s only loop is the deliberate per-version half-install probe).
- **DNF itself is tuned** — `play-basic-configs.yml:229-232` sets `max_parallel_downloads=10`.
- **The CCY Dockerfile is otherwise well-ordered**: heavy apt/npm layers first, volatile `COPY`s (patch script, entrypoint, docs, skills) last; `--no-install-recommends` plus apt cache cleanup on every layer; the PHPantom Rust build is isolated in its own stage so it caches independently of the runtime image.
- **`qa-ctrl-z-patch.bash` caches its Claude Code install** under `scripts/qa-ccy/node_modules/` with an explicit `--update` refresh flag — exactly the right pattern for an expensive npm fetch.
- **`qa-bash.bash` already batches shellcheck** via `xargs -0` (one process per ARG_MAX batch, not per file) and uses `--slurpfile` to dodge ARG_MAX on large JSON — the scan *mechanics* are efficient; only the scan *scope* is the problem (PERF-01).
- **Repo hygiene is excellent for performance**: 6.9 MB `.git`, 409 tracked files, no generated artefacts or node_modules committed.
- **Extension cleanup is correct** — every `timeout_add` source found is removed in `disable()`/`destroy()`, so no leaked timers accumulate across enable/disable cycles.
