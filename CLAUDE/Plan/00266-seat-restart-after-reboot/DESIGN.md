# Design research: seats and LXC containers that come back after the nightly reboot

Plan 00266's design research, read in the code on 26 Nov 2026 (v2-smoke, which is F44 plus the
teamchat v2 work). Every claim is marked **[checked file:line]** (read in the code) or
**[unchecked]**. Nothing here is built. The owner sees this first.

## Reply

**The coordinator's starting view is mostly right, but two of its premises are wrong.**

1. **ccy-sessions restore already covers host `cc`. It is not an open gap.** cc writes the same registry record (prefix `cc`), and restore replays it [checked cc:57-80, refusals.go:66-67, registry.go:256-268]. What cc lacks is resuming by its seats' record: `bySeats` is ccy-only [checked conversation/start.go:76].
2. **The record is already structured, but two of its fields are free-form and replayed as given.**
   - `launcher=` is an unvalidated absolute path, and restore execs it [checked restore.go:560].
   - `arg=` keeps every unknown flag with its value, and every word after `--` [checked replay.go:23-42].
   - Nothing is spliced into a shell: the trampoline runs it as `"$@"` [checked registry/restore.go:503-510, tmuxccy/start.go:82-84]. So the hazard is "exec any binary with any args", not shell injection.
   - The container cannot write the registry under the default mounts [checked claude-yolo:2467-2469; registry 0700/0600 at registry.go:199-202].

**The real boot-time holes are files in the checkout, which the container can write and which the host launcher reads unattended:**

- **`.claude/ccy/mounts`.** The container can write this file, and the host launcher applies it on the next launch with no approval step. Its host-side deny list does not cover every host path from which the host later runs code [checked common-pure.bash:446-451, common.bash:424-478]. A container could therefore widen its own next launch onto such a path. This is the class to close first.
- **Compose files under `--compose start`.** `up -d` runs on the host with the checkout's compose file [checked network-management.bash:941-950].
- **A checkout shared by a ccy seat and a host cc seat.** Its `.claude/settings.json` hooks would run on the host [unchecked].

**LXC:** the member session unit exists, and by the owner's ruling it is never enabled [checked members.py:25-33, 953-982]. Add a root system unit that records which containers are running at shutdown and, at boot, starts exactly those, plus the member units that were active. Order it Before `user@<uid>.service`.

**Size:** medium, about 9 tasks in 3 batches (section 4).

---

## 1. What `ccy-sessions restore` records and replays today

### The record format

- **Location:** `$CCY_STATE_DIR/sessions`, else `${XDG_STATE_HOME:-$HOME/.local/state}/ccy/sessions/<name>`. A relative base is refused [checked registry.go:101-133].
- **Text format, version 1:**
  - header `ccy-session-record 1`;
  - fields `name= dir= launcher= prefix= restore=yes|no`, optional `compose=started|declined|running` and `fresh-if-no-conversation=yes`;
  - `arg=` repeated, one per word [checked registry.go:20-27, 173-191].
- **Strict reader:** it rejects an unknown key, a missing field, a bad enum or a non-regular file [checked registry.go:339-418].
- **Write-side validation is only:** the name is non-empty with no `/`; dir, launcher and prefix are non-empty; compose is in the enum; no value holds a newline [checked registry.go:140-171]. Nothing checks that `launcher` is a known binary, that `prefix` is in {ccy, cc}, or that `dir` is anything in particular.
- **Who writes it:**
  - The bash library `ccy_registry_write` [checked session-registry.bash:243-293], from the one terminal-launch site `ccy_tmux_insulate` [checked tmux-session.bash:404-423].
  - `launcher` there is `$1`: the launcher's own resolved path, e.g. cc passes `$(cd "$(dirname "$0")" && pwd)/$(basename "$0")` [checked cc:68].
  - The Go side through `Store.Write`, `SetTeams` and `SetCCTeams` [checked registry.go:195-307], and `ccy-sessions declare` with a launcher it chooses [checked sessions/declare.go:241].
  - Everything runs as the owning host user, so any host process of that user can write it, including a host `cc` claude session [unchecked as a test; it follows from the 0700 user directory].
- **Can a ccy container write it?** Not under the default mounts. Only `$PWD:/workspace` and a read-only config temp directory are bound [checked claude-yolo:2467-2469].
  - The exceptions are `CCY_EXTRA_MOUNTS`, which is host env [checked claude-yolo:2477], and `.claude/ccy/mounts`, which the container can write; see section 3.
  - A ccy launched with `$PWD` equal to `$HOME` or an ancestor of it would expose the registry [unchecked whether ccy refuses that].

### The restore manifest

It is written beside the registry at `<state>/ccy/last-restore`, never inside it [checked restore/manifest.go:137-145]. It holds name, prefix, dir, resume, supervised, going, at, detail and transcript [checked manifest.go:36-37]. It is what `set-going` and `verify-restore` read. It is not replayed into argv.

### How the replay works

- `planRestore`: the dir must be a directory, then `conversation.StartPlan` runs [checked sessions/restore.go:155-170].
- `judgeRestore` adds: the hostname rule, `--ssh-key` must be a file, the token pool must not be all expired, the engine, and the network [checked restore.go:177-201, refusals.go:53-200].
- **argv:** `env CCY_SESSION_RESTORE=1 [passphrase-file var] <rec.Launcher> <args>` [checked restore.go:552-560], placed after `bash -c <trampoline> ccy-tmux` [checked tmuxccy/start.go:82-84]. The trampoline runs it as `"$@"`, and only the record path, stop path and prefix are quoted into it [checked registry/restore.go:503-519]. **No free-form string reaches a shell.**
- **Arguments:**
  - `ReplayArgs` drops the one-shot flags (`--prompt`, `--run-for`, `--until`, bare first messages and others) [checked replay.go:23-42, registry/restore.go:57-60].
  - It **keeps every unknown `-flag`, the word after it, and everything after `--` verbatim** [checked replay.go:31-41].
  - `startArgs` removes the conversation selectors and adds `--supervise`, `--compose` and `--resume <id>` [checked registry/restore.go:105-157].
  - For `cc`, every word is claude's on the host [checked registry.go:256-268]. So a cc record can carry `-- --mcp-config … / --settings …` to a host claude.
- **Conversation id:**
  - It is read from the session's teamchat record, or from the checkout's `.claude/ccy/seat-conversations/` for a seated ccy session [checked conversation/start.go:41-66, seatrecord.go:33].
  - It must match the UUID pattern [checked conversation.go:260-262, registry/restore.go:84-87, 105-108], and a transcript must exist [checked conversation.go:263-269].
  - Seat records are read and written through an `os.Root` in the checkout, so a link is refused [checked seatrecord.go:103-114, 200-228].

## 2. The smallest extension

### Host `cc` seats

These are already recorded and restored. The smallest change is:

- (a) Let `bySeats` resolve cc seats too. Today it is `KindCcy` only [checked start.go:76]. cc's checkout is the git top-level [checked conversation.go:283-297].
- (b) Apply the field validation of section 3 to the cc prefix. cc gets no hostname, ssh-key, compose or engine check, only the pool check [checked refusals.go:57-69].

There is no new record kind and no new unit. `cc-teams.bash` needs no change for the restore [unchecked beyond its use of the git top-level].

### LXC

- **Seats in LXC members:**
  - The restart mechanism already exists as a fixed, validated unit, `agent-bus-claude-<user>.service`. It is rendered from regex-checked values, has no `[Install]` section and `Restart=no`, and is started only on demand [checked members.py:25-33, 953-982].
  - The on-demand start is `ccy-teams` → `lxcfreeze.Lister.StartUnit` through sudo lxc-attach [checked teams/lxcsession/lxcsession.go:1-19, 129-166].
  - So an LXC seat's "startup command" is just `(container, user)`. Both are already declared in `agent_bus_lxc_members` [checked declare/lxc.go:14-24, 44-47].
- **Containers:**
  - `play-lxc-install-config.yml` enables `lxc.service`, which autostarts only `lxc.start.auto` containers [checked play-lxc-install-config.yml:76-82].
  - Nothing records which containers were running. There is no lxc snapshot unit in `files/etc/systemd/system/` [checked directory listing].
- **Proposal:** one root system unit pair in `play-lxc-install-config.yml`, because that play owns `lxc.service`.
  - `lxc-running-snapshot.service`: Type=oneshot, RemainAfterExit=yes, `ExecStop=` writes `/var/lib/fedora-desktop/lxc-running` (root-owned 0600). That file lists the container names running, and for each container the declared member units that were active.
  - It is ordered `After=lxc.service`, so at shutdown its stop runs before lxc.service stops the containers.
  - At boot the same unit's `ExecStart=` reads the file, starts exactly those containers (`lxc-start -n`), then the recorded member units.
  - It is ordered `After=lxc.service lxc-net.service network-online.target` and `Before=user@<uid>.service`. The user's `ccy-sessions-restore.service` [checked files/home/.config/systemd/user/ccy-sessions-restore.service] then runs after the LXC set is up. That matters only if a ccy or cc seat talks to an LXC-hosted service.
  - The member-unit start belongs in the Go tool (`lxcfreeze` or `ccy-teams internal`), not in bash, by the repo's bash limits.
  - The verify step, `fedora-desktop-self-update-verify.service` [checked .j2:15], should also report the LXC set.
- **Owner conflict:** starting member units at boot reverses the owner's Plan 00257 ruling that a session "never starts with its container" [checked lxcsession.go:3-5, members.py:28-30]. The owner must decide. Restoring only the units that were *active at shutdown* is the narrow reading that fits the new request.

## 3. Threat model: a compromised container trying to get code run on the host at the next boot

The container can write `/workspace`, which is the checkout, and anything mounted into it. The registry is not among those by default.

| Field                | Proposed validation at restore (and at write)                                                                                                                                                                                                                                                                                                                                                                                             | Today                                                                                                                                                                                                                                                                                                                                                         |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| kind/prefix          | enum {ccy, cc, lxc}                                                                                                                                                                                                                                                                                                                                                                                                                       | non-empty only [checked registry.go:157-161]                                                                                                                                                                                                                                                                                                                  |
| launcher             | **Derive it from the kind and drop the field.** Use the installed path (`~/.local/bin/ccy`, `/var/local/claude-code/cc`), or check the value against that fixed table.                                                                                                                                                                                                                                                                    | **Any absolute path is exec'd** [checked restore.go:560]. Not reachable from a container today, but it is the one field that turns a registry write into code execution.                                                                                                                                                                                      |
| dir (checkout)       | An existing directory that is a git work tree, resolved with `EvalSymlinks` and not a link. Not under any path a project mount or `CCY_EXTRA_MOUNTS` can bind. Not equal to or above `$HOME`.                                                                                                                                                                                                                                             | must be a directory only [checked restore.go:156]                                                                                                                                                                                                                                                                                                             |
| seats (`--teams`)    | each seat declared for this machine in `teamchat_teams` / `agent_bus_*`, of the record's kind                                                                                                                                                                                                                                                                                                                                             | kept as given (keepValue) [checked registry/restore.go:59]; launch-time checks refuse undeclared seats [unchecked at the restore step]                                                                                                                                                                                                                        |
| token                | a name matching `^[a-z0-9][a-z0-9_-]*$` that exists in the pool, never a path                                                                                                                                                                                                                                                                                                                                                             | kept as given [checked registry/restore.go:59]; how ccy resolves the value [unchecked, claude-yolo:807]                                                                                                                                                                                                                                                       |
| conversation id      | UUID with a transcript present                                                                                                                                                                                                                                                                                                                                                                                                            | **already held** [checked conversation.go:260-269]                                                                                                                                                                                                                                                                                                            |
| extra args           | An allowlist of ccy's keep and keep-value flags only. **Reject unknown flags and anything after `--`**, for cc above all, since its words reach a host claude (`--mcp-config`, `--settings`, `--plugin-dir`).                                                                                                                                                                                                                             | unknown flags and their values, and everything after `--`, are kept [checked replay.go:31-41]                                                                                                                                                                                                                                                                 |
| **checkout content** | **The real gap.** (1) Pin `.claude/ccy/mounts`: the host stores a hash of the approved file outside the checkout, and an unattended restore refuses a changed file. Also turn the source deny list into an allowlist. (2) Under `CCY_SESSION_RESTORE`, `--compose start` must refuse a compose file whose hash differs from the one the attended launch recorded. (3) Refuse a cc restore in a checkout that any podman seat also mounts. | (1) Denylist only, and it leaves out host paths the host later runs code from [checked common-pure.bash:446-451]; applied on every launch with no approval step [checked common.bash:424-478]. (2) `up -d` on the host from the checkout's file [checked network-management.bash:941-950, restore.go:215-246]. (3) [unchecked whether kinds share a checkout] |

**What today's code fails to hold:**

- A container that edits `.claude/ccy/mounts` can widen the binds of its next launch, attended or unattended, past what the deny list covers.
- An edited compose file runs as host rootless podman with whatever volumes it names.

Neither is boot-specific, but the boot makes both unattended. Under Defence Before Fix they are a class to name, put a detector on, and sweep: "host-side launcher reads container-writable checkout config". The registry record itself is not reachable from a container. Its `launcher` and `arg` fields are still the worst-case shape, so tighten them in the same change, even though it was not the coordinator's concern.

The LXC snapshot file must be root-owned under `/var/lib`. Restore must start only names that are both in the snapshot and declared in `agent_bus_lxc_members`, or that exist under root's lxcpath. Member units are started only by their derived name `agent-bus-claude-<user>.service`, with the user checked against `_USER_RE` [checked members.py:962-963].

## 4. Size and batch split

Medium, about 9 tasks:

- **Batch A (security first, Defence Before Fix):**
  1. A detector rule for the class.
  2. Pin and approve `.claude/ccy/mounts` and extend its deny list.
  3. Compose-file hash under restore.
  4. Derive `launcher` from the prefix and validate it.
  5. An argument allowlist at replay, with no post-`--` words for cc.
- **Batch B:**
  6\. `bySeats` for cc.
  7\. Checkout and token-name validation in `judgeRestore`.
- **Batch C (needs the owner's ruling on Plan 00257):**
  8\. The LXC snapshot and start-at-boot unit plus a Go subcommand in `play-lxc-install-config.yml`.
  9\. Extend verify-restore to report the LXC set.

Every play change goes into `meta-deploy.list`, with a ccy version bump for the claude-yolo edits.
