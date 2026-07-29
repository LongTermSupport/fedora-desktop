# Phase-2 Design Decision — Plan 00065 (T2.1–T2.3)

Reviewed at `becf5a7` (HEAD), against a freshly-pulled checkout (`git pull --ff-only` → already up to date). CCY container — edit/design only, no Ansible
run, no commit.

**DECISION 1: place the linger-enable task inside `play-systemd-user-tweaks.yml`
as its first task(s) — NOT a new play.**

**DECISION 2: FULL-HARDEN-NOW** — delete `ignore_errors`, turn the verify into
a hard `assert`, and pair it with an explicit synchronous `user@{{ uid }}.service`
start (not just `loginctl enable-linger`) so the hardening is deterministic
rather than merely "usually works."

---

## DECISION 1: home for the `loginctl enable-linger` task

### Verdict

Add the linger-enable task (plus the `getent` UID lookup it needs) as the
**first two tasks of the existing `playbooks/imports/play-systemd-user-tweaks.yml`**,
before the "Create Systemd User Config Directory" task. Do **not** create
`play-AC-user-linger.yml`, and do **not** put it in `play-basic-configs.yml`.

### Rationale

**Ordering requirement is already satisfied without touching `playbook-main.yml`.**
Execution order today (`playbooks/playbook-main.yml:5-11`):

1. `play-AA-preflight-sanity.yml`
2. `play-AB-dnf-upgrade.yml`
3. `play-basic-configs.yml`
4. `play-prevent-ssh-suspend.yml`
5. `play-network-wait-tuning.yml`
6. `play-mask-intel-lpmd.yml`
7. `play-systemd-user-tweaks.yml` ← first play that actually needs a running
   user manager on **every** profile
8. … (`play-podman.yml` is play #23, `playbooks/playbook-main.yml:29`)

`play-prevent-ssh-suspend.yml` (#4) touches D-Bus (`gsettings` via
`DBUS_SESSION_BUS_ADDRESS`, `playbooks/imports/play-prevent-ssh-suspend.yml:38-49`)
but that task is gated `when: provisioning_profile != 'server'`
(`play-prevent-ssh-suspend.yml:49`) — it never runs headless, so it imposes no
linger requirement. The **first** unconditional `systemctl --user` consumer on
server is `play-systemd-user-tweaks.yml`'s oomd verify/handler (#7). Enabling
linger as that play's own first task is therefore timing-sufficient for both
downstream consumers: the oomd tasks later in the *same* play, and
`play-podman.yml`'s `podman.socket` enable 16 plays later (#23) — no reorder
of `playbook-main.yml` needed at all.

**A new play fails the repo's own bar for creating one.**
`CLAUDE/AgentNotes.md` ("Review PRs holistically against the full IaC system"):
*"Only create a new play when the work has a genuinely independent lifecycle
(different `become`, host group, or opt-in story)."* Enabling linger has no
independent lifecycle here — it exists **solely** to make `systemctl --user`
reachable, which is exactly what `play-systemd-user-tweaks.yml` already is
(its own header: "SYSTEMD USER SERVICE TWEAKS", its own verify task literally
calls `systemctl --user show user.slice`). Bolting the linger/user-manager
bring-up onto the front of that play is the more self-documenting choice: a
reader opens the file and sees "first we ensure the user bus is up, then we
configure it" in one place, rather than splitting one concern (user-session
bring-up) across two files.

**The `AA-`/`AB-`/`ZZ-` prefixes are reserved bookend-anchor names, not a general
convention to extend.** Only three files in the whole tree carry a letter
prefix: `play-AA-preflight-sanity.yml` (must run literally first),
`play-AB-dnf-upgrade.yml` (must run second, before anything else touches
packages), and `play-ZZ-repo-cleanup.yml` (must run last). Every other play —
including `play-podman.yml`, `play-firefox.yml`, `play-systemd-user-tweaks.yml`
itself — is ordered purely by its position in `playbook-main.yml`'s
`import_playbook` list, not by filename. Minting a new `AC-` anchor for a task
that has no first/last positional requirement (it only needs to precede play
#7, which it already does by simply living inside play #7) would be inventing
a naming precedent the repo has not established, for no ordering benefit.

**`play-basic-configs.yml` (#3) was the other early candidate and is
defensible but strictly worse.** It is timing-safe (earlier than #7) but it is
a grab-bag of user dotfiles/SSH-helper/package tasks
(`playbooks/imports/play-basic-configs.yml:17-315`) with no thematic
connection to systemd user-session bring-up — adding it there spreads the
"make `systemctl --user` work" concern across two unrelated files for zero
ordering gain, since nothing between play #3 and play #7 needs a live user
bus today (`play-prevent-ssh-suspend.yml`'s D-Bus task is server-skipped, and
`play-network-wait-tuning.yml`/`play-mask-intel-lpmd.yml` are not session
plays as far as this review's scope covers).

**Scope and safety.** `play-systemd-user-tweaks.yml` already declares
`scope: general` with no scope guard (`play-systemd-user-tweaks.yml:8`) —
correct, since enabling linger is harmless and desirable on the desktop
profile too (this is the exact mechanism `play-rclone.yml:166-171` already
uses there). No new `when: provisioning_profile != 'server'` guard is needed
on the linger task itself; it runs unconditionally on both profiles, same as
every other task already in this play.

---

## DECISION 2: FULL-HARDEN-NOW

### The decisive technical question

> With `loginctl enable-linger` enabled, is a `become_user` `systemctl --user daemon-reload` at Ansible firstboot reliably going to succeed?

**No — not from `enable-linger` alone.** But the gap is closable
deterministically with one extra, well-precedented task, which is why the
verdict is FULL-HARDEN-NOW rather than deferring.

#### Does `enable-linger` block until the user manager is up?

No. `loginctl enable-linger` is a synchronous D-Bus method call
(`SetUserLinger`) to `systemd-logind`, but logind's handler only **queues** the
job that starts `user-runtime-dir@UID.service` (creates `/run/user/UID`) and
`user@UID.service` (the per-user manager, listening on
`/run/user/UID/systemd/private`) — it does not block the D-Bus reply on that
job's completion. The method call returns once the file
`/var/lib/systemd/linger/<user>` is written and the start job is *submitted*,
not once the user manager is *running*. This is a genuine, real async
start-up race for a user who has **never logged in** (the exact Cloud Base
firstboot case this plan targets) — there is no session already holding the
manager open.

This repo's own `play-rclone.yml` cannot be read as evidence against the race:
it does `loginctl enable-linger` (`play-rclone.yml:166-171`) immediately
before `become_user` `scope: user` systemd calls
(`play-rclone.yml:196-212`), and this has evidently worked in production —
but `play-rclone.yml` is an **optional, opt-in** play that runs against a
desktop user who, in every realistic invocation, already has an active
graphical/SSH session (and therefore an already-running `user@UID.service`
and populated `/run/user/UID`) *before* the play ever runs. `enable-linger` in
that context only marks persistence on an already-live manager — it never
has to cold-start one. The Cloud Base firstboot case is the opposite: no
session has ever existed, so the async job really is the first thing that
would bring the manager up, and nothing today waits for it.

#### Which env var actually matters: `XDG_RUNTIME_DIR` or `DBUS_SESSION_BUS_ADDRESS`?

**`XDG_RUNTIME_DIR` is the one `systemctl --user` needs.** `systemctl --user`
connects directly to the private control socket at
`$XDG_RUNTIME_DIR/systemd/private` — it does not go through the D-Bus session
bus at all. `DBUS_SESSION_BUS_ADDRESS` matters for genuine D-Bus clients like
`gsettings` (dconf talks over the real session bus), which is why
`play-prevent-ssh-suspend.yml:38-41` sets only `DBUS_SESSION_BUS_ADDRESS` —
its one `become_user` task is a `gsettings` call, not a `systemctl --user`
call, so it is not a counter-example; it simply never needed
`XDG_RUNTIME_DIR`.

The repo's **actual, load-bearing precedent for `systemctl --user` under
`become_user`** is `play-rclone.yml:155-158` and `:203-205`/`:300-302`, which
sets **both** vars on every `scope: user` systemd task and documents exactly
this failure mode in a comment: *"`systemctl --user` from `become_user` needs
`XDG_RUNTIME_DIR` pointing at the user's runtime dir ... Without this,
`state: restarted` fails with an empty 'Unable to restart service' error."*
Use that exact two-var shape — it costs nothing to include
`DBUS_SESSION_BUS_ADDRESS` alongside `XDG_RUNTIME_DIR` for robustness (some
`systemctl --user` subcommands and any future D-Bus-touching addition to this
play would need it), and it keeps every `scope: user` task in the repo
visually consistent.

This also explains why the *current*, unhardened code has not visibly broken
before: `play-systemd-user-tweaks.yml`'s verify task and handler set **no**
`environment:` block at all, and this repo's connection defaults to running
`ansible-playbook` as the desktop user via `sudo -HE`
(`CLAUDE/AnsibleStyle.md:9`) — the `-E` flag preserves the invoking shell's
environment, so on a real desktop, self-provisioning run, `become_user: {{ user_login }}` often coincides with the *same* user whose already-exported
`$XDG_RUNTIME_DIR` rides through via `-E`. On a Cloud Base firstboot, `run.bash`
runs from cloud-init/root context with no such variable to preserve, and
`become_user` drops into a manager that was never started — this is a
headless-only failure mode with no desktop-side warning sign, exactly the
audited "silent misbehave" class this plan is fixing.

#### Making it deterministic (not a poll/retry)

Prefer an **explicit, blocking start** over a race-prone wait loop:

```yaml
- name: Ensure the user's systemd manager is running (headless firstboot)
  become: true
  ansible.builtin.systemd:
    name: "user@{{ ansible_facts.getent_passwd[user_login][1] }}.service"
    state: started
  # `ansible.builtin.systemd: state: started` shells out to `systemctl start`
  # without --no-block, which blocks until the job completes — for a
  # Type=notify unit like user@.service that means blocking until the user
  # manager signals READY=1, not merely "job submitted". This makes the
  # manager's presence (and therefore /run/user/<uid> and its private socket)
  # a guarantee at the next task, closing the loginctl enable-linger async
  # race documented above. user@.service ships with the base systemd package
  # and depends only on user-runtime-dir@%i.service, so this has no extra
  # package/service prerequisite beyond what already exists on Cloud Base.
```

This is preferable to a `wait_for: path=.../systemd/private` poll or a
hand-rolled `until` shell retry: it is one declarative module call (no
`shell:`, so none of the Ansible 2.19 parser gotchas in `CLAUDE/AgentNotes.md`
apply), it fails loud and immediately if the unit genuinely cannot start
(fail-fast, not a bounded-but-silent poll timeout), and `enable-linger` +
`state: started` together are fully idempotent (`creates:` on the linger
marker file; `systemd: state: started` is a no-op if already running) so the
task is safe on every re-run.

#### Recommendation and justification

**FULL-HARDEN-NOW.** The repo's fail-fast culture is not the only argument
here — the plan's own success criteria already commit to this outcome
("the one existing `ignore_errors` in the core tree is removed, not
re-justified"). The reason FULL-HARDEN-NOW is *correct*, not just cautious, is
that the investigation above found a **genuinely deterministic** fix, not a
"probably fine" one: explicitly starting `user@UID.service` as root blocks on
the systemd job itself, so there is no residual timing window for the hard
`assert` to trip over. Deferring to CONSERVATIVE-DEFER would mean shipping a
profile-aware `failed_when: false` precisely in the one place this plan exists
to fix, on the theory that the fix might be flaky — but the flakiness was
only ever inherent to relying on `enable-linger`'s async trigger alone, which
this design does not do. Reintroducing suppression here would be optimizing
for a risk that the explicit-start task already retires.

The one caveat worth stating plainly: this reasoning is architectural (no
Ansible run available in the CCY container per `CLAUDE/ContainerRules.md`).
The HOST test (Phase 4, Task 4.2) is still the actual proof; if it surfaces a
Cloud-Base-specific reason `user@.service` cannot start synchronously (none
is known — it is a base `systemd` package unit with no extra dependency),
that would be new information, not something this review overlooked.

#### T2.3 — should `play-podman.yml`'s `dbus_session_check` probe be replaced?

**Yes, delete it and depend on T2.1+T2.2 instead.** Today's task
(`play-podman.yml:14-25`) is exactly the probe-then-conditional anti-pattern
`CLAUDE/AgentNotes.md` calls out ("Reject patterns that probe for
known-installed software instead of asserting/depending on it... Probe-then-
conditional patterns hide that knowledge and create drift"): it runs
`systemctl --user status` **with no `become_user` and no `environment:` at
all**, so on a desktop self-provisioning run (ansible connects as
`user_login` already) it happens to see a live session and passes; under
cloud-init/root firstboot it silently reports `rc != 0` and the socket enable
is skipped with no signal that anything is wrong — the same "green run, wrong
box" failure class as T2.2's oomd config. Once `play-systemd-user-tweaks.yml`
(play #7) has synchronously started `user@UID.service` and its linger marker
is in place, `play-podman.yml` (play #23) can rely on that guarantee the same
way `play-rclone.yml` does: add `become: true`, `become_user: "{{ user_login }}"`, and the `XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS`
`environment:` block to the "Enable Podman Socket" task itself, drop the
`when: dbus_session_check.rc == 0` guard, and delete the `dbus_session_check`
task entirely (it becomes dead code once nothing branches on it).

---

## Implementation checklist

1. **`play-systemd-user-tweaks.yml`** — insert as the new first two tasks
   (before "Create Systemd User Config Directory"):
   - `ansible.builtin.getent: database: passwd, key: "{{ user_login }}"`
     (no `when`, no tags needed beyond consistency with the rest of the play).
   - `ansible.builtin.command: cmd: loginctl enable-linger {{ user_login }}, creates: /var/lib/systemd/linger/{{ user_login }}`, `become: true`.
   - `ansible.builtin.systemd: name: "user@{{ ansible_facts.getent_passwd[user_login][1] }}.service", state: started`,
     `become: true` (the explicit synchronous start from Decision 2).
2. **Handler `reload-systemd-user-daemon`** (`play-systemd-user-tweaks.yml`,
   currently the lone `ignore_errors: true` in the core tree) — delete
   `ignore_errors: true`; add:
   ```yaml
   environment:
     XDG_RUNTIME_DIR: "/run/user/{{ ansible_facts.getent_passwd[user_login][1] }}"
     DBUS_SESSION_BUS_ADDRESS: "unix:path=/run/user/{{ ansible_facts.getent_passwd[user_login][1] }}/bus"
   ```
3. **"Verify systemd-oomd Configuration" task** — add the same `environment:`
   block; replace `failed_when: false  # FAIL-FAST-OK: ...` with a real
   assertion, e.g. follow the shell task with:
   ```yaml
   - name: Assert systemd-oomd Configuration Applied
     ansible.builtin.assert:
       that:
         - "'ManagedOOMMemoryPressure=auto' in oomd_verify.stdout"
       fail_msg: "systemd-oomd override not applied — see oomd_verify.stdout"
   ```
   (keep `changed_when: false` on the shell task; it is a read, not a
   mutation).
4. **`play-podman.yml`** — delete the "Check if user D-Bus session is
   available" task (`dbus_session_check`); on "Enable Podman Socket for
   Docker Compatibility", add `become: true`, `become_user: "{{ user_login }}"`, the same two-var `environment:` block, and drop
   `when: dbus_session_check.rc == 0`. Add the `getent` lookup if
   `play-podman.yml` cannot assume it already ran (it can — `getent`'s
   `ansible_facts.getent_passwd` fact persists for the whole play run once
   set in an earlier play in the same `ansible-playbook` invocation; if
   `play-podman.yml` is ever run standalone rather than via
   `playbook-main.yml`, add its own `getent` task defensively since standalone
   runnability is a stated repo requirement, `CLAUDE/AnsibleStyle.md`
   "Provisioning Profile Self-Guard").
5. Run `ansible-playbook --syntax-check` on both edited playbooks (CCY-safe);
   full `./scripts/qa-all.bash` and the live HOST test remain Phase 4 items
   per the existing plan.
