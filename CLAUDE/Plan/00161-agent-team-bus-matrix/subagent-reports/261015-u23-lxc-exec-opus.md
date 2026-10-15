# U23.lxc: `env: 'pingbus': Permission denied` (exit 126), 26-10-15

## Cause

The cause was not the pingbus file's mode. The installer puts `kit/pingbus` with an explicit
0755 (`put_file ... 0755`), and the play's own LXC path (`helpers/agent_bus_lxc/members.py`)
creates the kit's directories 0755 and chmods them explicitly. Plan 00201's commits did not
change either one.

What changed is the umask of the host run. Every `_meta-deploy` and acceptance run directory up
to and including 26-10-14 is 0755 with 0644 files. From the first run on 26-10-15, they are
0700 with 0600 files. The passing U23.lxc run (26-10-14) was made under umask 022, and the
failing run under 077. The `cc` wrapper sets `umask 077`, so a shell opened from a desktop
Claude session would carry it. That is consistent with the evidence but not proven.

sudo ORs its 022 with the caller's umask, and lxc-attach keeps the result, so root in the member
also ran under 077. The acceptance's README step 1 copy (`U23_GUEST_PREPARE`) made the kit's
parent directory with `mkdir -p` and extracted the kit with tarfile's `"data"` filter. That
filter drops directory modes, so new directories take the umask. `/usr/local/share/agent-bus`
and `kit/` were both made 0700, and the agent user could not reach the `/usr/local/bin/pingbus`
symlink's target, which gives EACCES and exit 126.

The docker and vm members start their processes without the host's umask. They were not reached
in this run, because lxc runs first and failed.

## Fix

The fix is in `_acceptance-u23.inc.bash`. The new snippet `U23_KIT_UNPACK_PY` sets the parent
directory and every kit directory to 0755 explicitly. It also fails if the tar did not make
`kit/pingbus`. The run no longer depends on the owner's umask, and the owner does not need to
change anything on the host.

## Tests

- `test_u23_kit_unpack.py` sources the include and runs the snippet under umask 077. It checks
  that the directories are 0755 and that the file modes are kept.
- In this container the test skips: it has Python 3.11.2, which has no tarfile extraction
  filters. Run it on the host.
- shellcheck passes on the include and on `acceptance.bash`, and `qa-changed.bash` was run.
- `acceptance.bash` is already queued in `meta-deploy.list`.
