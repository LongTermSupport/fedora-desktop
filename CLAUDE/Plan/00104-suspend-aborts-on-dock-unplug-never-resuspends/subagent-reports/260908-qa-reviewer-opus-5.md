# QA Review — commit `34d9879` "Plan 00104: make a suspend request durable, not just bounded"

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44

**Verdict**: BLOCK

## Blocking

### 1. The move into `playbook-main.yml` puts an ungated UPower task on the headless-server path — it will abort the whole run

`playbooks/imports/play-suspend-and-lid-policy.yml:38-46`, and the `restart-upower` handler at `:111-114`.

```yaml
- name: Configure UPower to ignore lid (let logind handle it)
  ansible.builtin.lineinfile:
    path: /etc/UPower/UPower.conf
    regexp: '^IgnoreLid='
```

No `create:`, no `when: provisioning_profile != 'server'`. Evidence:

- `rpm -qf /etc/UPower/UPower.conf` -> `upower-1.91.3-1.fc44.x86_64`, so the file exists only if upower is installed.
- `grep -rn "upower" playbooks/ --include="*.yml"` returns nothing outside this play — **no playbook in the repo installs upower**.
- `lineinfile` without `create` fails with `Path ... does not exist !`, and `ansible.cfg` sets `any_errors_fatal = true` -> whole-RUN abort, not just this play.
- `environment/localhost/group_vars/desktop.yml` defines `provisioning_profile` for the *same* `desktop` group a server lives in, so `hosts: desktop` does not exclude a server.

This is new breakage. Before this commit the play was under `imports/optional/hardware-specific/` and `playbook-main.yml` never imported it, so a server profile never reached it. The play *already knows* about this hazard — the gsettings task at `:92-95` carries the Plan 00061 6.2 comment about exactly this abort mode — the guard was simply not generalised to its neighbour.

**Fix**: gate the UPower task and its handler on `provisioning_profile != 'server'` (or on a `stat` of the file).

Checked and clear on the sibling risk: `blockinfile` with `create: true` does `os.makedirs(destpath)` (`ansible/modules/blockinfile.py:274-280`), so the `logind.conf.d` task is safe even though `rpm -qf /etc/systemd/logind.conf.d` reports the directory is unowned by any package.

## Should fix

### 2. `systemd-run` with a fixed `--unit=` and no `--collect` — one failed retry wedges layer 2 permanently

`files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend:83-88`.

`man 1 systemd-run`, `--collect`: *"Normally, without this option, all units that ran and failed are kept in memory until the user explicitly resets their failure state with `systemctl reset-failed`."* The unit name is hardcoded, so once `resuspend-aborted-suspend-retry.service` is loaded in failed state, every later `systemd-run --unit=resuspend-aborted-suspend-retry` fails (`Failed to start transient %s unit`, confirmed present in `strings /usr/bin/systemd-run`) — and with `set -e` the hook then exits non-zero into a place nobody reads.

The most likely way it fails is documented in this very plan: `TRIAGE-EVIDENCE.md` F12 — `ssh-suspend-guard` holds a **block**-mode sleep inhibitor while any inbound SSH session exists, and the retry runs plain `systemctl suspend` (no `-i`, correctly), which logind refuses. `PLAN.md:241-242` lists that interaction under Dependencies and the hook does not account for it.

**Fix**: add `--collect`, and log the retry unit's outcome rather than fire-and-forget.

### 3. Layer 1 is inert until reboot: the udev rule is reloaded but never triggered

`playbooks/imports/play-suspend-and-lid-policy.yml:116-122`. `udevadm control --reload-rules` does not re-apply rules to devices already present, and `AC` / `ucsi-source-psy-*` are never removed and re-added on a running system.

The repo already has the correct two-step pattern six directories away — `playbooks/imports/optional/hardware-specific/play-displaylink.yml:336-343`:

```yaml
- name: reload-udev-rules
  ...
  notify: trigger-udev
- name: trigger-udev
  ansible.builtin.command: udevadm trigger --subsystem-match=usb
```

**Fix**: `notify: trigger-udev` with `udevadm trigger --subsystem-match=power_supply`. The play compensates with a "REBOOT REQUIRED" debug instead, which turns an applicable-now change into an unverifiable one.

(`changed_when: true` on the reload handler is correct and matches the sibling — not a finding.)

### 4. `docs/architecture.md` numbered import list was not updated

`docs/architecture.md:89-119`.

- `grep -c "^- import_playbook:" playbooks/playbook-main.yml` -> **31**
- `grep -cE "^[0-9]{2}\. \*\*play-" docs/architecture.md` -> **30**

`play-suspend-and-lid-policy.yml` is absent, and it belongs at position 05 (after `play-prevent-ssh-suspend.yml`), which renumbers 05-30 -> 06-31. `PLAN.md:212` marks "Update `docs/playbooks.md`" complete — that half was done correctly; the other doc that enumerates the same list was missed. This is the "partial result read as complete" shape from `CLAUDE/AgentNotes.md`.

### 5. The lid probe cannot tell "lid open" from "no lid interface", and asserts the former

Hook lines 60-72:

```bash
for f in /proc/acpi/button/lid/*/state; do ... done
if [[ "$lid_state" != *closed* ]]; then
    log "resumed after ${elapsed}s but the lid is open — user is present, leaving awake"
```

On any host without `/proc/acpi/button/lid/` the glob does not match, `lid_state=""`, and the hook logs a positive statement it did not observe while silently disabling layer 2. Since the play is now imported by `playbook-main.yml` for *every* host, that includes desktops and servers.

The plan already recorded the interface that does not have this problem — `TRIAGE-EVIDENCE.md` F13: `busctl get-property org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager LidClosed`, which returns `b false`/`b true` or an error. logind is a *system* service, so `man 8 systemd-suspend.service`'s "user.slice will be frozen" warning does not apply to querying it.

**Fix**: at minimum distinguish the unreadable case and say so in the log.

### 6. The attempt cap does not do what its comment says — "give up" is erased immediately

Hook lines 74-81:

```bash
if (( attempts >= MAX_ATTEMPTS )); then
    log "... giving up rather than looping."
    rm -f "$COUNT"
```

Deleting the counter on the give-up path means the give-up is not sticky: the very next suspend that aborts starts a fresh 3-attempt burst. With layer 3 now enabling `sleep-inactive-battery-type=suspend` at the existing 900s timeout, a persistently-waking machine on battery produces a repeating burst every ~15 minutes indefinitely — bounded, but not "giving up".

The reset the code needs already exists on the success path (line 55, `elapsed > RESUSPEND_WINDOW -> rm -f "$COUNT"`); the one in the give-up branch is redundant and harmful.

### 7. `RESUSPEND_WINDOW=30` is not derived from the evidence, and its justification is an unevidenced generalisation

Hook lines 20 and 30-32: *"A human waking their own machine essentially never does so within seconds of asking it to sleep."*

The plan's own measurement is much tighter — `TRIAGE-EVIDENCE.md` F1: `PM: suspend entry` at 11:40:49, abort at 11:40:52, i.e. ~3s; `PLAN.md:11-12` says "aborted three seconds later". A 30s window is an order of magnitude wider than the failure it targets, and every extra second is pure false-positive surface on the configuration the operator uses daily (docked, lid shut, external monitors — where `sleep-inactive-ac-type=nothing` means a suspend is *always* an explicit request).

Concretely: click Suspend, change your mind, wake from the external keyboard -> the hook re-suspends 5s later, three times, before it stops.

**Fix**: narrow the window to the measured few seconds, or add a positive signal that the wake was human (session became active / input activity) rather than inferring it from the clock.

### 8. A permanently deployed comment states as fact what the evidence file marks as an unverified premise

`files/etc/udev/rules.d/99-suspend-wakeup-policy.rules:8-9`: *"Plan 00104: a user-requested suspend was cancelled ~3s in by unplugging."*

`TRIAGE-EVIDENCE.md` F17 explicitly says the opposite: *"Note the counts are not proof of the abort... the attribution of the specific 11:40:52 abort remains premise P2."*

Worse, F1's timeline puts the **USB disconnect storm at 11:40:52** *before* the NVRM power-source change at 11:40:53 — so the rule's stated cause points at the devices it disarms while the first-logged candidate is the USB tree it deliberately leaves armed.

**Fix**: hedge the comment to what F17 supports ("these devices are armed wakeup sources; a wake event during the s2idle transition aborts the suspend") and drop the incident attribution.

### 9. An unconditional "Configuration updated successfully / REBOOT REQUIRED" banner now prints on every full deploy

`playbooks/imports/play-suspend-and-lid-policy.yml:97-108`. The `debug` task has no `when:` and no changed-gating. It was tolerable in an optional, hand-invoked play; now that `playbook-main.yml` imports it, every routine run claims a config update happened and demands a reboot. That is a wolf-crying change of blast radius introduced by this commit.

### 10. `PLAN.md` drift left by this commit's own edits

- `:240-241` Dependencies: *"Touches `play-prevent-ssh-suspend.yml`"* — it no longer does; `git show --stat 34d9879` does not list that file.
- `:26` *"facts numbered F1-F13"* — this commit added F17; the range is stale (F14-F17 exist).
- Decision 3 (`:147-167`) still reads as a live decision ("The setting goes in `play-prevent-ssh-suspend.yml`"). Its supersession is recorded only in the Phase 3 preamble at `:202-205`, not in the decision itself, so a reader landing on Decision 3 gets the wrong answer. Decision 4 annotates its own supersession of Decision 1 correctly — do the same for 3.

## Nits

- **Zero-byte state files pass the `-r` guard** — hook lines 47 and 76. An empty `$STAMP` makes `elapsed=$(( $(date +%s) - slept_at ))` a bash arithmetic syntax error, killing the hook under `set -e`; same for an empty `$COUNT`. This is the `CLAUDE/AgentNotes.md` "existence treated as generated" shape. Validate the content is a non-empty integer.
- **`[[ -r "$STAMP" ]] || exit 0` (line 47) is a silent, unlogged no-op.** If `pre` never wrote, layer 2 is off and nothing says so.
- **`playbook-main.yml:9-11` contradicts itself**: *"Must follow `play-prevent-ssh-suspend.yml`: ... Ordering is not functionally required (different keys)."* Say one thing — it is adjacency for readability, not a dependency.
- **Handler naming inconsistency**: `reload-udev` here vs `reload-udev-rules` in `play-displaylink.yml:336`. Same action, two names.
- **`hibernate` is absent from the hook's `case` (lines 39-42)** with no comment saying why. Resume-from-hibernate with the lid closed gets no recovery. Deliberate is fine; undocumented is not.
- **Mixed command qualification in the hook**: `/usr/bin/systemctl` is absolute, `systemd-run` and `logger` are not.
- **`docs/playbooks.md:117-118`** describes disarming AC/UCSI wakeup but not the corollary the user will actually notice: plugging in the mains will no longer wake a sleeping machine.
- **`TRIAGE-EVIDENCE.md:149`** still cites the pre-move path `playbooks/imports/optional/hardware-specific/play-laptop-lid-power-management.yml` in the present tense ("deployed by"). It is a capture-time record, so arguably correct, but a "(now `imports/play-suspend-and-lid-policy.yml`)" note would stop it reading as current.

## Checked and clean

- **Hook location.** `/usr/lib/systemd/system-sleep` is the **only** directory `systemd-sleep` scans — `strings /usr/lib/systemd/systemd-sleep | grep system-sleep` yields exactly one path, and `/etc/systemd/system-sleep` does not exist on this host. Deploying into `/usr/lib` is correct here, not an FHS violation.
- **`pre` reachability.** `man 8 systemd-suspend.service`: the *same* executables are run before and after every sleep operation, and the `case` at hook lines 39-42 accepts the identical `$2` set in both directions — no path reaches `post` without `pre` for a systemd-driven sleep.
- **Deadlock rationale is sound.** Same man page: *"All executables in this directory are executed in parallel, and execution of the action is not continued until all executables have finished"* — an inline `systemctl suspend` would indeed deadlock. `systemd-run` returns immediately. Residual, unverified: `RESUSPEND_DELAY=5` may fire while the *other* post hooks (`displaylink.sh`, `nvidia`, both present in `/usr/lib/systemd/system-sleep/`) are still running. Worth confirming in Phase 4 rather than assuming.
- **`/run` state across reboot.** `/run` is tmpfs; after a reboot the stamp is absent and `post` exits 0 at line 47. Correct, no stale-counter carry-over.
- **udev rule syntax and match keys.** `udevadm verify files/etc/udev/rules.d/99-suspend-wakeup-policy.rules` -> `Success: 1, Fail: 0`. `udevadm info --query=property` confirms `SUBSYSTEM=power_supply` and kernel names `AC` / `ucsi-source-psy-USBC000:00{1,2}`. `power/wakeup` exists and reads `enabled` on all three class devices, and is *absent* on `BAT0`, so the rule cannot mis-fire there. The parent ACPI/platform devices have no `power/wakeup`, and `/proc/acpi/wakeup` has no `ACPI0003` entry — so the class attribute is the right and only knob. F17's claims verified independently.
- **Does disarming AC/UCSI break anything?** `power/wakeup` gates wake capability only; charging is EC/hardware and unaffected. Nothing in the repo depends on wake-on-AC. The lost capability is "plug in mains to wake", a deliberate trade — see the nit about documenting it.
- **The move itself.** Correct per repo convention: `playbook-main.yml:3-4` forbids importing from `imports/optional/`; the file kept mode `100755` and the `#!/usr/bin/env ansible-playbook` shebang; `scope: general` with a per-task `provisioning_profile` guard matches `CLAUDE/AnsibleStyle.md:267-270`; `root_dir` uses the mandated config lookup. The gsettings task is byte-for-byte the pattern of its sibling `play-prevent-ssh-suspend.yml:51-67`. No stale references to the old filename outside historical plan documents.
- **Fail-fast.** No `failed_when:` / `ignore_errors:` anywhere in the diff; the hook opens `set -euo pipefail`; no multi-command `shell:` blocks.
- **Public-repo safety.** Targeted grep for usernames, domains, home paths, RFC1918 IPs, MACs and gmail addresses across the three deployed files and the whole plan diff: only match is the git author trailer. Hardware identifiers (`USBC000:001`, `ELAN0676`, `PNP0C0D`) are generic ACPI IDs, not identity.
- **Plan Commit Rule.** `git status --short` is clean; `PLAN.md`, `TRIAGE-EVIDENCE.md` and the `JOURNAL/` entry all landed in the same commit as the code. `README.md` needs no new row (plan pre-existed). Task 4.3 correctly still unchecked.
- **Bash gate coverage.** The extensionless hook *is* discovered — `qa_discover_shell_files` returns it among 199 files — and it produced no shellcheck findings.
- **Rename blindness.** `.git/hooks/pre-commit:45` uses `--diff-filter=d`, so the `git mv` + edit in this commit was scanned (the Plan 00081 row-5 defect is already fixed).

## Mechanical gates

- **`scripts/qa-all.bash`**: rc=2, failing only on the two pre-existing items. Confirmed not from this commit — `git show --stat 34d9879` touches neither `CLAUDE/Plan/00079-podman-container-control/unit-test-selection.bash` (8x SC2154) nor `.ruff-version` (pin 0.16.0 vs installed 0.16.3). Nothing in `34d9879` appears in the failure list.
- **`hooks-daemon plan-qa --sweep`**: rc=1, 0 block / 2 advise. Both advisories are repo-wide staleness / journal-freshness nags; Plan 00104 is in neither list.
- **`ansible-playbook --syntax-check`**: `playbooks/imports/play-suspend-and-lid-policy.yml` rc=0; `playbooks/playbook-main.yml` rc=0.
- **Conditional gates — not triggered, and checked**: no `helpers/` or `tests/helpers/` change -> `qa-helper-tests.bash` not required; no `extensions/` metadata or JS change -> `check_extension_compat` and ESLint not required; no `files/var/local/claude-yolo/**` change -> no `CCY_VERSION` / `REQUIRED_CONTAINER_VERSION` bump required.
