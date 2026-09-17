# Greeter power policy

Why the machine idle-suspended while plugged in, which dconf database actually governs
the greeter, and why a single `gdm.d` drop-in is sufficient.

## The finding

`sleep-inactive-ac-type` has three possible sources on this host, and they disagree:

| Scope                | Value                    | Where it comes from                                 |
| -------------------- | ------------------------ | --------------------------------------------------- |
| GNOME schema default | `'suspend'`, timeout 900 | Stock GNOME. Suspend-on-AC is the shipped default   |
| The human user       | `'nothing'`              | Set explicitly by `play-prevent-ssh-suspend.yml`    |
| `gdm` (the greeter)  | `'suspend'`              | **Nothing is set — it inherits the schema default** |

Read with `Gio.Settings.get_default_value()` against
`org.gnome.settings-daemon.plugins.power`, which distinguishes a configured value from a
default. `sleep-inactive-battery-type` defaults to `'suspend'` as well.

Nobody misconfigured the greeter. It was never configured at all, and the GNOME default
is the behaviour nobody wanted.

## Why this defeats the repo's stated intent

`play-prevent-ssh-suspend.yml` carries the task **"Disable suspend on AC power (plugged
in = never idle-suspend)"**, whose documented purpose is that inbound SSH survives while
the machine is plugged in. `play-suspend-and-lid-policy.yml` sets the battery sibling,
and its comments state the asymmetry is deliberate: *"The AC sibling … is deliberate and
lives in play-prevent-ssh-suspend.yml so inbound SSH survives — do NOT 'make them
consistent'."*

That intent is sound. Its **implementation reaches exactly one of the two accounts that
can own the display.** Both plays apply their setting with
`become_user: {{ user_login }}`, driving `gsettings` over that user's session bus. The
`gdm` account is touched by neither — no play in the repository mentions `gdm`,
`/etc/dconf/db/gdm.d`, or a greeter profile.

The consequence is that the policy is not a property of the host, it is a property of one
user's session. The moment that session ends — crash, logout, or switch-user — the
unmanaged account takes over and the policy inverts. A machine left at the login screen
is a machine with suspend-on-AC enabled, which is precisely the state the SSH guard
exists to prevent.

## The "inert fix" concern, investigated and withdrawn

This document previously recorded that a `/etc/dconf/db/gdm.d/` drop-in would very likely
be **inert**, on the grounds that `/etc/dconf/profile/gdm` does not exist and so nothing
would declare the `gdm` database worth reading. That reasoning was sound but the premise
was incomplete, and the conclusion is **wrong**.

`/etc/dconf/profile/` is not the only profile location. GDM ships its profile with the
package, and it is present on this host:

```
$ cat /usr/share/dconf/profile/gdm
user-db:user
system-db:gdm
system-db:local
system-db:site
system-db:distro
file-db:/usr/share/gdm/greeter-dconf-defaults
```

`system-db:gdm` is already in the stack, and it is the **highest-priority system database**
in it. So a `gdm.d` drop-in is read, and it outranks `local`, `site`, `distro` and the
greeter's own shipped defaults. Nothing needs creating under `/etc/dconf/profile/`.

The state of the databases confirms nothing currently competes for this key:

| Path                                    | State                                                                                            |
| --------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `/usr/share/dconf/profile/gdm`          | **Present** — stacks `system-db:gdm` first among the system databases                            |
| `/etc/dconf/profile/gdm`                | Absent, and does not need to exist                                                               |
| `/etc/dconf/db/gdm.d/`                  | Empty except an empty `locks/` — this is where the drop-in goes                                  |
| `/etc/dconf/db/gdm`                     | Compiled, 104 bytes, holds no settings (the same size as the empty `local` and `site` databases) |
| `/usr/share/gdm/greeter-dconf-defaults` | Contains **no** power keys                                                                       |
| `/etc/dconf/db/distro.d/`               | Contains **no** power keys                                                                       |

With no database in the stack setting the key, the greeter falls through to the GNOME
schema default. Read back in the greeter's own scope, that is exactly what is observed:

```
$ sudo -u gdm env DCONF_PROFILE=gdm gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type
'suspend'
$ sudo -u gdm env DCONF_PROFILE=gdm gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-timeout
900
```

900 seconds is the 15-minute delay between the greeter appearing and the suspend, matched
to the second in [incident-chain.md](incident-chain.md).

**Task 2.1 is therefore answered: a `gdm.d` drop-in alone is sufficient.** The fix is one
part, not two.

## Whether a lock is required — it is not

A dconf **lock** prevents a higher-priority database from overriding a system value. In
this profile the only thing above `system-db:gdm` is `user-db:user`, which for the greeter
resolves to `/var/lib/gdm/.config/dconf/user`. That database exists (736 bytes) and holds
power-**profile** and notification keys, but **not** `sleep-inactive-*` — and the greeter
has no UI that would write one, since it never runs gnome-control-center.

So a lock would defend against a write that nothing performs. It is omitted on YAGNI
grounds, and this paragraph is the record of *why* rather than an oversight. **Task 2.2
answered: no lock.**

The falsification for that decision is cheap and belongs in the read-back: if the
post-change read-back in the greeter scope ever returns `'suspend'` again while the
drop-in is present, a user-db value has appeared and the lock becomes load-bearing.

## The second-order finding

Neither play re-reads what it set. Both `set` tasks are `changed_when: false` with no
read-back; the only `gsettings` probe present is a **precondition** check that the schema
is readable, not a confirmation that the value took. The existing verification block runs
`helpers.suspend_wakeup.cli`, which covers the udev wakeup policy and not these keys.

A read-back assertion on the user-scope keys would not have caught this particular gap —
the user-scope value was correct throughout. But the absence of that habit is why a
policy could be half-applied for as long as it has been without anything noticing. Any
greeter change should ship with the read-back that proves it, and the existing user-scope
sets deserve the same treatment. Recorded as Tasks 3.1 and 3.2.

### The read-back specification (Task 3.2)

The gap is in `play-prevent-ssh-suspend.yml`, task *"Disable suspend on AC power"*
(`:51-67`). It issues `gsettings set … sleep-inactive-ac-type nothing` over the user's
session bus, carries `changed_when: false`, and **nothing ever reads the key back**.
`gsettings set` exits 0 whenever the schema resolves, so a write that lands in the wrong
place — a stale `DBUS_SESSION_BUS_ADDRESS` from a UID that has since changed, or a dconf
database a later profile outranks — is indistinguishable from one that took. The play then
ends with *"Verify ssh-suspend-guard is running"*, which asserts a **different** thing and
makes the play look verified.

Add immediately after the set task, inside the same
`when: provisioning_profile != 'server'` guard and with the identical `become_user` and
`environment:` block — the read must cross the same bus as the write, or it proves nothing
about it:

```yaml
- name: Read back the AC suspend policy
  become: true
  become_user: "{{ user_login }}"
  ansible.builtin.command:
    argv:
      - gsettings
      - get
      - org.gnome.settings-daemon.plugins.power
      - sleep-inactive-ac-type
  environment:
    DBUS_SESSION_BUS_ADDRESS: "unix:path=/run/user/{{ ansible_facts.getent_passwd[user_login][1] }}/bus"
  register: ssh_suspend_ac_readback
  changed_when: false
  failed_when: ssh_suspend_ac_readback.stdout | trim != "'nothing'"
  when: provisioning_profile != 'server'
```

`failed_when` compares against `'nothing'` **with its quotes**, because that is what
`gsettings get` prints for a string; stripping them would also accept a bare `nothing`
from a differently-typed key. This is the probe-then-fail shape the fail-fast rule
permits, not a `failed_when: false` suppression.

Deliberately **not** specified: a matching read-back for
`play-suspend-and-lid-policy.yml`'s host-scope keys. Task 3.1 already specifies one for
the greeter key it adds, and inventing assertions for keys this incident never touched is
scope this plan has no evidence for.
