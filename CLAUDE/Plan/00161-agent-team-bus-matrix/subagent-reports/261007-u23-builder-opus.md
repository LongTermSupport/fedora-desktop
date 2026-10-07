# U23 builder report: other encapsulations on the same host (M3a)

Branch `agent-addca231a09e5952e-a856c768` (pushed; not merged into F44). The commit is the
branch tip whose message starts "Plan 00161: U23".

## What was built

| File                       | Change                                                                                                                                   |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `_acceptance-u23.inc.bash` | New. The whole slice: preparing each member, discovering its bridge, widening `allow_from`, the three checks, teardown, `needs_owner`.   |
| `acceptance.bash`          | Additive: header, usage and exit-code text; `--vm-ssh=`; sources the include; `check_leg` outcome 3; `finish_needs_owner`; the U23 legs. |
| `acceptance_check.py`      | Three commands: `host-subnet`, `allow-from`, `suggested-args`.                                                                           |
| `test_acceptance_check.py` | 16 new tests (3 classes), written first and seen failing.                                                                                |
| `PLAN.md`                  | M3 line: U23 built, host run pending.                                                                                                    |
| `JOURNAL/…26-10-07.md`     | One `action` entry through `mkplan.bash --journal`.                                                                                      |

`_acceptance-steps.inc.bash` is untouched, so the merge with U20 should only meet in
`acceptance.bash`, where every U23 change is a block of added lines. The one shared line that
changed is the final `ACCEPTED:` message, which now names M1 and U23.

## Design choices, and why

**Members join the existing acceptance team, not a second team.** M1's member a stays in the
team until the final teardown, so it can be the desktop-side member without being set up again.
Widening the team also tests something new: `agent-bus-install team` is run a second time with a
larger `allow_from`, and the run then depends on the installer applying that change (the
firewalld rich rule in the right zone, the unit's `IPAddressAllow`, the restart). M1's setup
does not change. The team is widened only after M1's checks, so a missing or broken engine
cannot affect M1. A second team would have needed member_run, the human and the teardown
repeated for another team name, all of which `_acceptance-steps.inc.bash` fixes to `TEAM`.

**`allow_from` is found at run time, never written down.** Each member runs a one-line Python
UDP `connect()` towards `<bus_ip>`, which sends nothing, and reports the source address its
kernel picks. `acceptance_check.py host-subnet` then reads `ip -j -4 addr show` and finds the
single host network that holds that address. That interface must be a bridge
(`/sys/class/net/<dev>/bridge`), because every README says the member is routed through one.
A guest whose traffic leaves from a host address (passt) fails here with a message that says
so. It is not admitted. The found network is added to the team file (`allow-from`), and the
team is applied again.

**Each member follows its README, not a workaround.** The steps are numbered as in
README.lxc, README.docker and README.vm:

1. The kit is copied to `/usr/local/share/agent-bus/kit` as a tar, and `pingbus` and
   `agent-bus-claude` are linked into `/usr/local/bin`.
2. The bridge subnet is added to `allow_from`.
3. `HOOKS_DAEMON_HOSTNAME=acceptance pingbus suggest-handle` runs as the agent user in its
   checkout. `suggested-args` requires exactly one `--repo --host --type` line, and the
   `--type` must be the member's real encapsulation (`lxc`, `docker` or `vm`). Those three
   arguments go to `add-member` unchanged, with `--role=worker --address=<bus_ip>`.
4. The agent user extracts its own bundle, so it owns the 0700 directory and 0600 files. The
   run directory's copy of the token is deleted.
5. The env file is created at `~/.config/pingbus/env` (0600). For docker, the container
   environment is used instead: `PINGBUS_TEAMS`, `PINGBUS_HOME`, `HOOKS_DAEMON_HOSTNAME`.
6. `pingbus config check` runs, then the first `recv` (the join, which must exit 3).

After that, member a sends a `review` (the M1 path reference). The member's background `wait`
must print exactly that PING (`expect-ping`). The member then sends an `ack --re`, and a's
`recv` must print exactly that ack (`expect-ping`).

The agent user's commands source the env file when it exists, as `agent-bus-claude` does, so
the env file's contents are what make `config check` pass. Starting Claude through
`agent-bus-claude` is not run here: it needs a logged-in `claude`, which belongs to U20.

**LXC: a throwaway container, created by the run.** It is
`lxc-create -t download -- --dist fedora --release <host VERSION_ID>`, following Plan 00122's
precedent, and needs an image download from the LXC image server. The container is named
`agent-bus-acceptance-u23`; a container left by a failed run is destroyed at the start, and the
container is destroyed on every exit path. Inside it,
`dnf install python3 git-core util-linux-core shadow-utils systemd` (a package download)
provides what README.lxc and suggest-handle need: Python, git (suggest-handle reads the
checkout with it) and `systemd-detect-virt`.

**Docker: a throwaway container from `python:3.13-bookworm`, pinned by its multi-arch index
digest** (`sha256:073ffebb…9ff4`, read from Docker Hub when this was built). The slim image
has no git, and `suggest-handle` refuses to run without it, so the full image is used. It is
large, about 1 GB. The acceptance pulls it. Moving the pull into deploy.bash, as with H2's
busybox, would have meant editing deploy.bash for no gain. It runs on docker's default bridge
with README.docker's three environment variables, and is removed on every exit path. Only
rootful Docker counts (`/usr/bin/dockerd`), because the podman-docker shim is not a docker
member.

**VM: driven over SSH when the owner names a guest; otherwise SKIPPED-NEEDS-OWNER.** Nothing in
the repository can run a member inside a VM automatically:

- The vmtest lab (Plan 00110) runs only manifest scenarios through `run.bash` (`vmtest run <scenario>`). It has no verb that boots a base and runs arbitrary commands.
- Its guests use rootless `qemu:///session` with passt, so they would not reach `<bus_ip>`
  through a libvirt bridge as README.vm describes.

Adding a "boot and exec" verb to vmtest would be a feature of its own (a deployed tool with its
own version, drift pairs and bridge allowlist) and would still test the wrong network path.

What was built instead takes the guest from `--vm-ssh=<user>@<address>`, or from
`agent_bus_acceptance_vm` in the untracked host_vars (read with `ansible-inventory`, the same
way the bus address is read). It runs the same README steps through `ssh -o BatchMode=yes`
and `sudo -n`. In the guest:

- the agent user `agent-bus-u23` is created and later removed;
- a marker file beside the kit means teardown removes only what this run created;
- a guest that already has an agent-bus kit, the kit's command links or that user is refused,
  and nothing in it is changed.

Without a guest, the leg returns SKIPPED-NEEDS-OWNER with the exact owner step (below).

**SKIPPED-NEEDS-OWNER is outcome 3, separate from the others.**

- A check calls `needs_owner "<step>"`, which appends to `OWNER_NEEDS`, then returns 3.
- `check_leg` prints and reports `SKIPPED-NEEDS-OWNER <name>: <step>` and does not exit, so the
  remaining checks still run.
- After the final teardown, `finish_needs_owner` writes a "NOT ACCEPTED: SKIPPED-NEEDS-OWNER"
  block listing each step to the log and the report, then exits 3.
- The `ACCEPTED` line is printed only when nothing was skipped.

Exit code 3 is new (0, 1, 2 and 64 already existed), and the header and usage say what it
means. `needs_owner` is defined in the U23 include, because ShellCheck reports SC2329 when a
function that is only called indirectly is defined in `acceptance.bash`, and suppressions are
banned. Its comment says any slice may use it.

**Cleanup on every exit path.**

- A leg that removes leftovers runs right after M1's (`u23_teardown leftover`). It covers the
  LXC container, the docker container, what this acceptance made in the guest, and the run
  directory's bundle and token copies.
- `plan_on_cleanup u23_teardown_after_stop` is registered after M1's teardown.
- `u23_teardown final` runs before M1's `teardown final`, which purges the team and with it
  every member account.
- `U23_PRESENT[kind]` is set before each attempt to create something, so a half-created
  container is still removed.

## What each leg does on the host

1. `remove U23 members left by an interrupted run`: resolves the VM. If a guest is named, the
   removal script runs in it (it must answer over SSH, or the leg fails). Then
   `lxc-destroy -f` and `docker rm -f` for the fixed name, wherever one exists.

2. (M1 runs unchanged.)

3. `U23: an LXC member's throwaway container`:

   - lxc-create (image download), start, wait for RUNNING;
   - wait up to 60 s for an IPv4 lease;
   - dnf install inside;
   - the guest-prepare script (agent user, checkout, kit and links);
   - the source address and the bridge (`lxcbr0` is expected).

   Without lxc-create or the download template, it records the owner step instead.

4. `U23: a docker member's throwaway container`: docker pull of the pinned image, docker run,
   the same prepare script, then the source address and bridge (`docker0` is expected). Without
   dockerd, it records the owner step.

5. `U23: a VM member's guest, as the owner gave it`: `sudo -n true` over SSH, the prepare
   script, then the source address and bridge (the libvirt bridge is expected).

6. `U23: the members' bridge networks join allow_from`: `acceptance_check.py allow-from` on the
   team file, then `agent-bus-install team` (label `team-u23`). This step does nothing when no
   member was prepared.

7. `U23.lxc`, `U23.docker`, `U23.vm` (check legs): either SKIPPED-NEEDS-OWNER, or the README
   steps 3 to 6, then the review, the wait and the ack described above. Evidence lines go into
   acceptance-report.md, and each member's stdout and stderr are kept in
   `members/<lxc|docker|vm>/<label>.{out,err}`.

8. `U23: remove the LXC, docker and VM members`, then M1's team removal, then
   `finish_needs_owner`, then `ACCEPTED`.

## What the owner must provide

- **For the VM leg** (it is SKIPPED-NEEDS-OWNER, and the plan cannot close, until this is
  done), a libvirt guest on this host's system network: a bridge, such as libvirt's default
  network under `qemu:///system`. The guest needs:

  - python3 3.12 or later (3.11.4+ also has the tarfile data filter the bundle extraction
    uses), git, systemd, sudo and sshd;
  - bash as the login shell (Fedora's default), because the SSH commands are `%q`-quoted.

  On the desktop side:

  - this desktop user must reach the guest's login user by SSH key with no prompt, with the
    host key already in known_hosts;
  - that login user must have passwordless sudo;
  - set `agent_bus_acceptance_vm: <user>@<guest-address>` in
    `environment/localhost/host_vars/localhost.yml`, or pass `--vm-ssh=`.

  The guest must have no `/usr/local/share/agent-bus`, no `/usr/local/bin/pingbus` and no
  `agent-bus-u23` user.

- **Docker leg**: rootful Docker, through `playbooks/imports/optional/common/play-docker.yml`,
  if it is not installed. H2 showed that docker is installed on the host.

- **LXC leg**: nothing new. `play-lxc-install-config.yml` is core, and H2 used LXC. The run
  downloads a Fedora LXC image and dnf packages.

- Network access for the docker image pull, the LXC image and dnf, on top of GitHub's API,
  which M1 already needs.

No `meta-deploy.bash` change was needed: 00161 is already in `PLANS`, and the owner's existing
`./CLAUDE/Plan/meta-deploy.bash` run now includes U23.

## Tests and QA

- `python3 -m unittest` in the plan folder: test_acceptance_check went from 33 to 49 tests,
  all passing. The 16 new tests were written first and failed (29 errors and 3 failures,
  counting subtests) before the code was written. The whole plan folder suite: 198 tests,
  all passing.

- `shellcheck -x acceptance.bash _acceptance-u23.inc.bash _acceptance-steps.inc.bash`: clean.
  `bash -n` passes on both changed scripts.

- An offline smoke test (untracked scratch, not committed) used stubs. It confirmed:

  - the `%q` SSH quoting round-trips spaces, quotes, `$` and newlines through `bash -c`;
  - both guest scripts parse under `sh -n`;
  - the agent env wrapper sources the env file only when it exists;
  - the env-file writer creates the file 0600 and refuses to overwrite an existing one;
  - `needs_owner` records its step.

  The bundle extraction one-liner could not run here: the container's Python is 3.11.2, which
  has no tarfile data filter. It is the same call M1 uses on the host.

- Nothing ran on a host: no LXC, docker, VM, installer or acceptance run happened in the
  container.

## Open questions

1. **LXC image contents.** The dnf list assumes the Fedora LXC image may lack python3, git or
   runuser. If the image's dnf cannot reach a mirror from the container, the leg FAILs. That
   is the correct verdict, but the LXC side of H2 only proved the bus address can be reached.
2. **Docker source address.** H2 saw the docker bridge's own address as the source, and
   `host-subnet` finds docker0's network from the container's address either way. If docker
   ever changed the source to the host's primary address (userland-proxy), the homeserver's
   filter would refuse it, and the check would fail rather than pass by accident.
3. **The VM path is semi-automatic.** The owner provides the guest, and the run does every
   README step inside it. Should a vmtest "boot a base on the system network and exec" verb be
   planned so U23 (and U24, which also needs a Fedora test VM) can run unattended? That is a
   decision for the owner; U24 will meet the same question.
4. **`agent_bus_acceptance_vm`** is read from the untracked host_vars, but it is not
   documented in `localhost.yml.dist`. It is a plan-local acceptance knob, not a deployed
   setting, so it is documented in `acceptance.bash`'s header and usage and in the
   SKIPPED-NEEDS-OWNER message instead. If the owner would rather have it in the `.dist`,
   that is a one-line addition.
5. **Merging with U20.** Both slices add legs after M1.3 and edit the final `ACCEPTED` line.
   Whoever merges second resolves those few lines. U20 can use `needs_owner` and exit 3 for
   its own owner steps (a named team and checkout).
