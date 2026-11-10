# Stages B and C (Plan 00209 Tasks 4.1 and 4.2): report

## Task 4.1: done, commit 9e13ce887 (not pushed)

CCY 3.114.0, container 2.81.0. The ccy image carries the v2 kit at
`/opt/claude-yolo/optional/teamchat` (`bin/teamchat`, `plugin/teamchat/` with the four hooks and
the `teamchat:teamchat` skill, `settings.json` with the status line).

- `tasks/teamchat-ccy-kit.yml`, imported by `tasks/claude-yolo-image.yml` after the v1 kit,
  stages the kit. The binary is built static (`CGO_ENABLED=0`, `-trimpath -buildvcs=false`,
  `-mod=vendor`) for the host's architecture (no literal) from **src-go as committed at HEAD**:
  `git archive` of the `HEAD:src-go` tree into a scratch directory, with that tree id as
  `-X main.SourceDigest`. So the digest names the bytes built, and a dirty checkout (other
  agents' uncommitted src-go) neither enters the image nor fails the ccy play. Proven in the
  container: archive, build and `teamchat version` printing the tree id.
- The rest of the kit is committed under `files/opt/claude-yolo/optional/teamchat/`.
  `src-go/internal/teamchat/member/kit/image_test.go` holds those files equal to
  `kit.Assemble`'s output for `paths.CCYKitDir()`, and allows no other file (red before the
  files existed, green after).
- Dockerfile: `COPY optional/` already takes it in. The build now runs
  `teamchat version`, so an image without a working kit binary fails to build. `teamchat` is
  **not** linked onto PATH: D 3.6 puts that link in the launcher for a v2 session.
- `scripts/test-ccy-container-version-hook.bash`: the play walker skips a `block:` wrapper (its
  tasks are walked one by one) and knows the built-binary copy (`BUILT`). It now proves each of
  the four new kit files needs a container bump (125 passed, 0 failed).
- `meta-deploy.list`: the existing `changed-plays` entry's description now names the ccy play's
  image rebuild for this. Host agent: run `./CLAUDE/Plan/meta-deploy.bash`. Live proof after
  it: in a ccy session, `/opt/claude-yolo/optional/teamchat/bin/teamchat version`.
- PLAN.md Task 4.1 ticked; journal entry `JOURNAL/2026-11-10/162234Z-action-0c80fb.md`.

QA: `qa-changed --files <mine>` was red only on files that are not mine: go-qa on
`internal/teamchat/richtext/` (untracked, Plan 00241's lane: gosec G104 x20, QF1001) and
`internal/vmtest/link.go:146` (`project_walk_open` in `readLinkPorts` and `memberRecord`, the
VM-lab lane). My one finding (two v1 words in a comment of the new task file) is fixed and
`qa-teamchat-separate` passes. `golangci-lint` on the kit package: 0 issues;
`--syntax-check` of `play-claude-yolo.yml` passes.

### Findings for the coordinator

1. **The stage B detector is not built.** INSTALL-PATH.md has the launcher compare the image's
   digest with `installed.json`'s kit digest. No program writes `installed.json` yet (stage A
   refuses it by name), so that waits.
2. **The container-version gate does not judge src-go.** A `teamchat` source change reaches the
   image on the next ccy play run (the build context changes and the build is re-run), but no
   container bump is asked for. The same is already true of `pingbus-status-line`, built
   through `build-go-command.yml`. The digest records which source the image has. Whether a
   bump should be demanded for the `cmd/teamchat` closure (most of `internal/teamchat`) is for
   the owner or coordinator to decide. INSTALL-PATH.md already leaves it to qa-reviewer.
3. **Dogfooding fault, filed as issue #231**: `lint_on_edit` and the staged lint gate report
   `'root_dir' is undefined` for any task file that statically imports by `{{ root_dir }}`
   (seen on `tasks/claude-yolo-image.yml`, on every edit and at commit). It is a false failure.

## Task 4.2: not started. What it needs and what bounds it

I stopped at 4.1 because of the context budget. These are the facts the next agent needs.

**The bash ratchet bounds the wiring.** `files/var/local/claude-yolo/claude-yolo` (2717 code
lines) and `entrypoint.sh` (440) are on `scripts/bash-limits.allowlist` and may not grow by a
line. Every v2 launch step therefore goes into Go. Bash may only branch where it already calls
something:

- Mounts and environment: `ccy_membership_args` in `lib/agent-bus-membership.bash` is already
  called by the launcher and fills `CCY_SEAT_RUN_ARGS`. It is not on the allowlist, so it must
  stay at 60 code lines or fewer and 4 functions or fewer. It has little room left. A v2 call
  there should be one line that execs Go and reads its argv back.
- Exit cleanup: `ccy_bus_disconnected` is already called from the launcher's cleanup
  (`claude-yolo:2359`). Seams 54 and 27, and RS3, can hang off it.
- **entrypoint.sh** has to exec `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <supervisor> --arm -- claude` for a v2 session (seam 53) and link `/usr/local/bin/teamchat`.
  That needs lines the file cannot grow by, so an offsetting port out of entrypoint.sh comes
  first. This is a decision for the coordinator: which piece moves to Go to make room.

**Go pieces that do not exist in production yet** (the library halves do exist:
`seat.Store.Take`/`Release`/`Announce`, `seat.Password`, `seat.VersionsReach`, `sweep.Sweep`,
`paths.SessionSeatMount`, and `ends.restartSwitch`, still false):

1. Seam 55: `ccy-teams system` (D 3.9). It is one resolving function for each team's system, and
   it refuses a session that asks for teams on both systems (OD-D2 (a)), naming the two choices.
   The input it reads on a member machine with no host_vars v2 team is not settled in the
   design. The cross-network pilot will have this case.
2. A launch take: open `seat.Store` (`StateNoRoster` where there is no status record, as
   `checkState` allows), choose the seat (C 3.7.4), `Take` into `sessions/<session>/`, then
   write `control/<session>/desired.json` (C 3.7.5, generation 1, `inbound`). It prints the two
   bind sources. Nothing in production writes ccy's `desired.json` today.
3. Seam 53: the mount guard. It refuses any other mount whose real source is an ancestor of,
   equal to, or inside the seat store root. It can be pure Go over the launch's `-v` list.
4. Adapters the store needs: `seat.Sessions` (is the session record still there; this is v1's
   00203 H5 record, so whether v2 shares the ccy session registry should be confirmed) and
   `seat.Announcer`, which runs `teamchat seat disconnected --bundle <pool>/held --since --seed`
   as the owner. `placeissued` stubs both as not used.
5. Seams 54 and 27 at exit: remove the session record first, then `Release` each held seat. For
   a crashed container whose record is still in place: `Announce` and keep the seat.
6. RS3: for a relaunch (supervisor exit 75), skip `seat disconnected` and record
   `held/notice.json` for that run. The same commit sets `ends.restartSwitch = true`, flips
   RS1's pinned test, and closes PLAN.md's 2.0e row.
7. `ccy-teams seats sweep` and the `teamchat-seats` user units (C 3.7.5, DC21) are needed so
   that a crashed launcher is still announced. They are not in Task 4.2's text, but the
   release path depends on them.

Gate B: a pool seat's password reaches a process only when the host runs it (Task 4.4). Nothing
above needs the owner before it merges.
