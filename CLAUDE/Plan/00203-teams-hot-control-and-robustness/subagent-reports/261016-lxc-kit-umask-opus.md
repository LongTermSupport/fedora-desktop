# LXC kit made untraversable by an inherited umask (fix/lxc-kit-umask)

## Failure

play-agent-bus.yml, `helpers.agent_bus_lxc.members --phase present`:
`<user>@<container-B>: pingbus suggest-handle in <checkout-B> failed (exit 126): env: pingbus: Permission denied`.

## Root cause

`helpers/agent_bus_lxc/members.py`, `KIT_SYNC_PY` (line 131 before the fix):
`os.makedirs(parent, mode=0o755, exist_ok=True)`. lxc-attach passes on its caller's umask
(077 in that run), so `/usr/local/share/agent-bus` was created 0700. The kit directories
under it got an explicit chmod, but the parent did not. `guarded()` refused only a parent
that others could write, not one they could not traverse. So the agent user could not reach
`kit/pingbus`, even though that file is 0755.

The same pattern also hit parents that `os.makedirs` creates on the way: their mode comes
from the umask alone, and an explicit chmod on the leaf never reaches them. That covers
`/usr/local/share` and `/etc/systemd/system` (on the root side), and `~/.config` (agent side;
0777 under umask 0, so the files were not secret, but too open).

## Fix: one approach, and why

Every guest script that creates anything sets its own umask as its first statement:

- root's scripts use 022: `KIT_SYNC_PY` and `UNIT_WRITE_PY`;
- the agent's scripts use 077: `PLACE_PY` (bundle) and `WRITE_ENV_PY` (env file).

Every file and leaf directory still names its mode explicitly. The umask was chosen over
chmod-per-directory because only the umask governs the parents that `makedirs` creates.
`OBSERVE_PY`, `REMOVE_PY`, `SOURCE_PY` and `SYSTEMD_PROBE_PY` create nothing.
`derive.py`'s `ROOT_READ_PY` runs on the host and only reads.

Repair: `guarded()` is replaced by `directory()`, which handles the kit parent and every kit
directory:

- A symlink, or anything else that is not a directory, is refused, naming the path. Nothing
  is followed.
- A directory not owned by the kit owner (root), or one that group or others can write, is
  still refused, unchanged.
- A directory that is root-owned and closed to others but not 0755 is set to 0755. It is
  opened `O_NOFOLLOW|O_DIRECTORY` and changed with `fchmod`. The script says
  `repaired <path> from 0700 to 0755` on stderr and emits it as a change line. `_sync_kit`
  forwards it both as `CHANGED` and through `say` to the helper's stderr.

The next run therefore fixes the broken container without any hand edit. The parent is also
`lstat`ed before it is made, so a dangling symlink there is refused by name rather than
ending in a traceback.

## Paths hardened (inside the container)

| Path                                                             | Script                 | Mode under any umask                              |
| ---------------------------------------------------------------- | ---------------------- | ------------------------------------------------- |
| `/usr/local/share` (if it has to be made)                        | KIT_SYNC_PY            | 0755                                              |
| `/usr/local/share/agent-bus`                                     | KIT_SYNC_PY            | 0755; existing 0700 (root's) repaired             |
| `/usr/local/share/agent-bus/kit` and each subdirectory           | KIT_SYNC_PY            | 0755; existing tight mode repaired                |
| kit files (`pingbus`, `agent-bus-claude`, plugin files)          | KIT_SYNC_PY            | the tar's mode (already chmod'd)                  |
| `/etc/systemd/system` and its parents (if they have to be made)  | UNIT_WRITE_PY          | 0755                                              |
| `/etc/systemd/system/agent-bus-claude-<user>.service`            | UNIT_WRITE_PY          | 0644                                              |
| `~/.config` (if it has to be made)                               | PLACE_PY, WRITE_ENV_PY | 0700                                              |
| `~/.config/pingbus`                                              | PLACE_PY, WRITE_ENV_PY | 0700 when made (an existing one is never touched) |
| `~/.config/pingbus/<team>/` and `token`, `README`, `member.json` | PLACE_PY               | 0700 / 0600                                       |
| `~/.config/pingbus/env`                                          | WRITE_ENV_PY           | 0600                                              |

Secrets were never widened: the umask only removes bits, and every secret was already
created with an explicit 0600 or 0700. The new test proves this under umasks 000, 022 and
077\.

## Tests (red first)

`tests/helpers/agent_bus_lxc/test_members.py` has a new class, `UmaskTest`. Five of its six
tests failed before the fix, and one errored:

- under umask 077, the kit is traversable: `share`, `agent-bus`, `kit` and `kit/plugin` are
  0755 (red: `share` was 0700);
- a kit parent and a kit directory left 0700 are repaired, and this is said on both the
  CHANGED and stderr streams (red: the error);
- a symlinked parent is refused by name, and its target is untouched (red);
- a group-writable parent, or one not owned by root, is refused and not repaired (passed
  before the fix too: the existing rule);
- under umask 077, the unit directory chain is 0755 and the unit is 0644 (red: `etc` was
  0700);
- secrets stay private under umasks 000, 022 and 077 (red: `~/.config` was 0777 or 0755).

`python3 -m unittest tests.helpers.agent_bus_lxc.test_members tests.helpers.agent_bus_lxc.test_derive`: 206 tests, OK.
`./scripts/qa-changed.bash`: passed. qa-all was not run, as instructed.

## Version bump

None is needed. The helper runs from the checkout through play-agent-bus.yml, and it is not
under `files/var/local/claude-yolo/`. qa-changed asked for no bump.

## Deploy

`CLAUDE/Plan/meta-deploy.list`: `playbooks/imports/play-agent-bus.yml` was already the first
entry, so its comment now describes this fix. Every other entry is kept, and the acceptance
entry has not been restored.
