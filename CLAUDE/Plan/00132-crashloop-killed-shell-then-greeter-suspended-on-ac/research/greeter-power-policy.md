# Greeter power policy

Why the machine idle-suspended while plugged in, and why the obvious fix would do
nothing.

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

## Why the obvious fix would be inert

The reflex fix is a drop-in at `/etc/dconf/db/gdm.d/`. On this host that would very
likely change nothing:

| Path                              | State                                                                                                              |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `/etc/dconf/profile/gdm`          | **Does not exist**                                                                                                 |
| `/etc/dconf/profile/`             | Contains only `ibus` and `user`                                                                                    |
| `/etc/dconf/db/gdm.d/`            | Empty except an empty `locks/`                                                                                     |
| `/etc/dconf/db/gdm`               | Present, 104 bytes, but contains no settings — `strings` yields only `GVariant` and `/.locks`. Owned by no package |
| `/var/lib/gdm/.config/dconf/user` | 736 bytes; holds power-**profile** and notification keys. Does **not** contain `sleep-inactive-*`                  |

dconf resolves a profile by name — the greeter runs with `DCONF_PROFILE=gdm` — and reads
`/etc/dconf/profile/gdm` to learn which databases that profile stacks. With that file
absent, the `gdm.d` database has nothing declaring it should be consulted. Writing a
file into an unread database is the kind of fix that looks applied, passes a file-exists
assertion, and changes no behaviour.

So the change is at least two parts: create the profile that stacks the database, **and**
populate the database. Whether a `locks/` entry is additionally required is a separate
question — locks exist to stop a user-scope value overriding the system one, and `gdm`
currently has no competing value for this key, so a lock may be belt-and-braces rather
than load-bearing. Recorded as Task 2.2.

## Open question that decides the approach

**Does creating `/etc/dconf/profile/gdm` plus a `gdm.d` entry actually change the
greeter's effective value?** This is unverified and must not be assumed. The test is a
read-back in the greeter's own scope after `dconf update`, compared against the
pre-change reading of `'suspend'`. Until that read-back is observed, no file layout
should be written into a play as "the fix".

This matters beyond convenience: the same trap — a plausible configuration path that may
not be the live one — is recorded independently for the D-Bus quota in
[detection-gap.md](detection-gap.md). Two mechanisms, same failure mode.

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
