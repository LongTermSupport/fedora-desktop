# Plan 00109 acceptance — diagnosis of the 9 failures

Run: `untracked/plan-runs/00109-…/acceptance/20260917-090113/`
Deploy: `untracked/plan-runs/00109-…/deploy/20260917-085853/` (started 08:58, acceptance 09:01 — same session, no logout between them).

Method note: everything below is established from repo source, the vendored ansible
module source in this container, the container's own systemd unit files, and the two
run logs. The container's user systemd manager is `offline` (`systemctl --user is-system-running`), so nothing here was tested against a live user manager. Where I
could not establish something, it is marked **NOT ESTABLISHED**.

---

## 1. The status document (checks [1]–[5]) — pure cascade

`~/.local/state/fedora-desktop/host-status.json` has exactly one writer in the repo:
`helpers/host_health/login_report.py:163-166` (`status_document.write_atomic`), reached
from `main()` at `helpers/host_health/login_report.py:483-494` via `record_host_state`.

`login_report` is the `ExecStart` of only two units:

- `files<home>/systemd/user/host-health.service.j2:23` (desktop profile)
- `files<home>/systemd/user/host-health-collect.service.j2:20` (server profile)

This host is the desktop profile (deploy log lines 43-67: every server-branch task
skipped), so `host-health.service` is the only producer. It has never started (check
[12]). Therefore no document → checks [1], [2], [3], [4], [5] all follow mechanically.

**The gate cannot self-heal this.** Check [9] does run `login_report`, but with
`--no-handoff`, and that flag is documented at
`helpers/host_health/login_report.py:432-437` as "write no host state — neither the
handoff file nor the status document". Confirmed at line 483: the `record_host_state`
call is inside `if not arguments.no_handoff`. So re-running acceptance without a login
will reproduce [1]–[5] identically.

**Verdict: 5 cascade failures, one root cause = check [12].**

---

## 2. Check [11] — enabled but not named by `graphical-session.target`

### What the check asks

`CLAUDE/Plan/00109-…/acceptance.bash:685-698`:

- `systemctl --user is-enabled host-health.service` → reads the **filesystem** (the
  install symlink). It returned `enabled`.
- `systemctl --user list-dependencies graphical-session.target` → reads the **running
  manager's in-memory dependency graph** over D-Bus. It did not name the unit.

So the symlink is on disk in a search path (otherwise `is-enabled` could not say
`enabled`), and the live manager does not know about it. The two answers disagree
because they consult two different sources.

### Why the manager never learned about it — a real ordering defect

`playbooks/imports/optional/common/play-host-health-login-report.yml:174-184`:

```yaml
- name: Enable The Login-Time Health Report
  ansible.builtin.systemd:
    name: host-health.service
    scope: user
    enabled: true
    daemon_reload: "{{ hh_unit.changed or hh_server_removed.changed }}"
```

The `ansible.builtin.systemd` module runs the reload **before** anything else. From the
module source vendored in this container
(`<home>/.local/pipx/venvs/ansible/lib/python3.11/site-packages/ansible/modules/systemd_service.py:396-398`):

```python
# Run daemon-reload first, if requested
if module.params['daemon_reload'] and not module.check_mode:
    (rc, out, err) = module.run_command("%s daemon-reload" % (systemctl))
```

The `enabled` handling is far below, at lines 502-545. So on this run
(`hh_unit.changed` was true — deploy log line 32 `changed: [localhost]`), the sequence
was: **daemon-reload → create the `.wants` symlink → stop.** The reload happened against
a filesystem that did not yet contain the symlink it was supposed to pick up. The
`daemon_reload` on that task can never do the job the play intends it to do.

Deploy log line 41 shows the enable task itself reported `changed: [localhost]`, i.e.
the symlink was created. Acceptance ran 3 minutes later and the manager still did not
name it. That is the observed fact, independent of any theory about what `systemctl enable` does internally.

### Is `WantedBy=graphical-session.target` itself wrong?

`files<home>/systemd/user/host-health.service.j2:42`.

The mechanism is **not** inherently broken, and I have direct evidence against the
"graphical-session.target is not a valid WantedBy" concern:

- `/usr/lib/systemd/user/graphical-session.target` in this container is a plain target
  with `Requires=basic.target`, `RefuseManualStart=yes`, `StopWhenUnneeded=yes` and **no
  `[Install]` section**. `RefuseManualStart` blocks `systemctl start`; it does not block
  a `.wants` symlink pointing at it.
- The distro itself ships exactly this pattern one target over:
  `/usr/lib/systemd/user/graphical-session-pre.target.wants/ssh-agent.service` is a
  vendor `.wants` symlink into a graphical-session target. So a `.wants` entry on these
  targets is a supported, shipped idiom — it is pulled in when the DE activates the
  target.

The distinction that matters: vendor symlinks live under `/usr/lib` and are read when
the manager starts. This unit's symlink lives under `~/.config/systemd/user/` and was
created *after* the manager last read its units.

The upstream `systemd.special(7)` guidance discourages `WantedBy=graphical-session.target`
in favour of `PartOf=` plus a DE-specific target. I could **NOT** verify that wording
from this container — no `systemd.special(7)` man page is installed and no vendored copy
exists in the repo. I am not asserting it. **NOT ESTABLISHED.**

### Will a logout/login fix it?

**NOT ESTABLISHED from this container**, and this is the one thing worth checking on the
host before relying on a re-login:

- `play-host-health-login-report.yml:29-34` states that lingering is enabled by
  `play-systemd-user-tweaks.yml`. With lingering on, `user@example.com` — and therefore
  its stale dependency graph — **survives a logout**. The manager is not restarted by a
  re-login.
- Whether `graphical-session.target` is garbage-collected while inactive and re-loaded
  from disk (picking up the `.wants` directory) on the next login is a systemd unit-GC
  question I cannot answer without the live manager. `StopWhenUnneeded=yes` makes it stop
  at logout, which makes GC plausible but not certain.

There is a fix that does not depend on the answer: a `systemctl --user daemon-reload`
before logging out.

---

## 3. Check [12] — `host-health.service` has never started

`acceptance.bash:707-727` reads `ExecMainStartTimestamp`; it was empty.

This is expected on its own terms: the unit was first deployed at 08:58 and acceptance
ran at 09:01, with no logout in between. `20260917-085853` is the only deploy directory
for this plan, so this was its first deploy on this host.

But [12] will only clear after a re-login **if** [11] clears — the whole point of the
`.wants` symlink is that it is what fires the unit. So [12] is blocked behind [11], not
merely behind a logout.

---

## 4. Check [6] — the ledger BROKEN sentinel is INDEPENDENT and already fixed in code

The sentinel reads (`ledger-report.txt`):

```
BROKEN yes
BROKENWHY 2026-09-15T08:59:35Z ValueError: the play carries no source position, …
CLEARED no
```

- The message comes from `helpers/play_ledger/plugin_support.py:79-82` (`play_source`),
  written by `record_failure` → `store.mark_broken` at `plugin_support.py:117-138`.
- The cause is documented at `helpers/play_ledger/plugin_support.py:46-68`: ansible-core
  2.19 removed `ansible_pos` and moved the fact to an `Origin` tag; `source_position()`
  now reads the new shape first. That is issue #46, and the fix is in the tree.
- **The cause is demonstrably gone.** Check [7] PASSED and the ledger holds 15 lines with
  `MALFORMED -`, including a row for every play this deploy ran
  (`play-fedora-desktop-panel.yml`, `play-host-health-login-report.yml`,
  `play-displaylink.yml`, …). Recording is working now.
- The sentinel is dated **2026-09-15**, two days before this deploy, and by design it
  never clears itself (`check_freshness.py:160`, and the operator instruction in
  `CLEAR_COMMAND` at `plugin_support.py:30-33`).

So this is a **stale marker from before the fix**, not a live defect. It has nothing to
do with `host-health.service`. It is the sole cause of the `play-freshness` finding in
check [9] too (`login-report.stderr.txt`).

Remedy is the documented one-liner, run by a human in the checkout:

```
python3 -m helpers.play_ledger.check_freshness --clear-broken
```

Not a code change. Not fixed by a logout.

---

## 5. Check [15] — the extension is a cascade, and expected

`helpers/gnome/extension_state.py:36` and `:71-81`: `PENDING_SCAN` means "a session is
live and GNOME Shell has no record of this UUID — it is on disk and has not been
scanned." Its `exit_code` is **0** — the play does not fail on it
(`extension_state.py:50-53`).

The deploy log confirms this was the extension's first deploy:

- line 88 `TASK [Deploy The fedora-desktop Panel Extension] … changed`
- line 91 `TASK [Declare The fedora-desktop Panel Enabled] … changed`
- line 99-101 the play itself prints "Log out and log back in. On Wayland that is the
  ONLY way to load the new extension code."

Check [13] passed (all 5 files deployed, UUID merged into `enabled-extensions`) and
check [14] passed (the deployed reader names the producer's path). So everything that
can be true before a shell restart is true.

**Why Plan 00112 reported 9 extensions ACTIVE and this one did not:** those 9 were
already on disk and already scanned when the current GNOME Shell session started. This
one was written to disk 3 minutes before the gate ran, inside a live Wayland session that
cannot rescan. The session does load extensions; it just cannot load one that appeared
after it started.

Worth noting for the gate's own design: check [15] as written is **unsatisfiable in the
same session as a first deploy**. That is a deliberate assertion of "loaded", and the
plan's own footer says to log out and re-run — but it means a clean deploy+acceptance
sequence can never be green in one pass.

---

## 6. Minor accuracy note (not a failure)

`host-health.service.j2:34-39` justifies `TimeoutStartSec=120` with "without this a hung
probe would hold `graphical-session.target` in `activating` for ever". With
`WantedBy=` (a Wants, not a Requires) plus `After=graphical-session.target` (line 9), the
target does not wait on this unit — it is ordered *after* the target is active. The
setting is still correct hygiene; the stated reason overstates the consequence.

---

## Summary table

| #    | Failure                             | Cascade of               | Code defect?                                                                | Re-login fixes?                                                                          |
| ---- | ----------------------------------- | ------------------------ | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| [1]  | no status document                  | [12]                     | no                                                                          | only if [11] is fixed first                                                              |
| [2]  | reader could not use the document   | [12]                     | no                                                                          | as [1]                                                                                   |
| [3]  | 4 sections missing                  | [12]                     | no                                                                          | as [1]                                                                                   |
| [4]  | no installed-vs-pinned state        | [12]                     | no                                                                          | as [1]                                                                                   |
| [5]  | no play-freshness state             | [12]                     | no                                                                          | as [1]                                                                                   |
| [6]  | ledger BROKEN sentinel              | independent              | **no — already fixed**, stale marker                                        | no; needs the `--clear-broken` command                                                   |
| [11] | enabled but target does not name it | independent              | **yes** — `daemon_reload` on the enable task runs before the symlink exists | unknown (manager persists under lingering); a `daemon-reload` fixes it deterministically |
| [12] | never started                       | blocked behind [11]      | no                                                                          | yes, once [11] is cleared                                                                |
| [15] | extension not loaded                | independent but expected | no                                                                          | yes                                                                                      |

## Recommended next action

Have the operator run, in the checkout, in this order:

```bash
systemctl --user daemon-reload
systemctl --user list-dependencies graphical-session.target | grep host-health
python3 -m helpers.play_ledger.check_freshness --clear-broken
```

If line 2 now names `host-health.service`, [11] is confirmed as the reload-ordering
defect and a logout/login will clear [1]–[5], [12] and [15] in one pass. Then log out,
log back in, and re-run `./acceptance.bash`.

The code change to make is in
`playbooks/imports/optional/common/play-host-health-login-report.yml`: split the
`daemon_reload` out of the enable task into its own task placed **after** the enable
(or add a second reload task after it), so the running user manager actually re-reads
`graphical-session.target.wants/`. The same defect exists on the server branch at lines
263-276.
