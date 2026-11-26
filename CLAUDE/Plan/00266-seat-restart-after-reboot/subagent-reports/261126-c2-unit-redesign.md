# Task C.2, the boot unit redesign through lxc@: report (26 Nov)

Builds on `261126-c2-unit.md` (the handoff), in the working tree of v2-smoke.

## Built, test first

- **The container start is the host systemd's.** `lxc start-recorded` no longer runs `lxc-start`.
  `lxc.Deps.Units` (`lxc.HostUnits`: `Start(ctx, unit)`) starts `lxc@<container>.service`. The
  unit name is built only after `paths.ParseContainerName`, so no `@`, `/`, `\`, space, leading
  `-` or `.` reaches it (`containerUnit`, `TestContainerUnitName`). `lxccmd.LXC.Start`, its test
  and the `lxc-start` row of `qa/rules/table.go` are gone; those three files are back to HEAD.
- **Wired from `units.Manager.Containers()`, NOT from `sys.Host.Systemd`.** This departs from
  the brief, for a reason: `host.Systemd` (`units.hostUnits`) refuses every unit but
  `teamchat-{server,tls,restore,restore-ctl}@` (`checkUnit`, SD8/SD10). So a wiring from
  `sys.Host.Systemd` would pass every fake and refuse every container on a real host. The new
  `units.ContainerUnits` starts an instance of `lxc@` whose name fits the container grammar, and
  nothing else. It has no Stop, Enable or Restart. `host.Systemd` still refuses `lxc@`. Tests:
  `TestContainersStartsAnLXCInstance` and `TestContainersStartsNothingElse` (units), and the type
  assertion in `TestLXCDepsAreTheHosts` (admin/wire).
- **A bounded wait for RUNNING** after each start, on `Deps.Clock`: every `BootRunningGap` (1 s),
  for at most `BootRunningWait` (60 s). A container still not running, an lxc-info failure, or a
  context that ends is `REFUSED`, naming the container and its last state. Tests:
  `TestStartRecordedWaitsForRunning`, `...WaitIsBounded` and `...WaitEndsWithItsContext`.
- **Shutdown order:** the drop-in `/etc/systemd/system/lxc@.service.d/teamchat.conf`
  (`paths.LXCContainerDropInFile`, unitfiles name `lxc@.service.conf`), `[Unit]`
  `Before=teamchat-lxc-record.service`, with a golden and `TestTheContainerDropIn`. The play has
  a new task, `Make the Directory of LXC's Unit Drop-in` (root 0755, before `Write Each Unit`,
  because `copy` makes no directory), held by installplay
  `TestThePlayMakesTheLXCDropInDirectoryFirst`.
- **Both units carry `v1names.InaccessiblePaths`.** The v1names want-list holds both services,
  and the drop-in is judged as a file that runs nothing. The goldens are regenerated, and the
  no-start golden now uses the six-paths placeholder too.
- `paths`: `HostVarLXCStartsContainers` is in `sampleNames`. `LXCContainerDropInFile` is new and
  sampled.
- `admin` fakes and the `units list`/`units print` expectations name the drop-in.
- `docs/agent-bus-lxc-members.md`, the `meta-deploy.list` entry for play-teamchat, and
  PLAN.md C.2 are updated.

## The variable is renamed

`paths` `TestEveryNameIsDerivedFromTheProduct` (D 3.1) failed once the name was sampled:
`lxc_seat_restore_starts_containers` is not formed from `teamchat`. On the lead's decision the
variable is renamed **`teamchat_lxc_seat_restore_starts_containers`** everywhere: the paths
constant, the play's input doc, variable and check, the jinja suite, the docs, PLAN.md C.2 and
the comments. `notDerived` is not touched. The jinja suite went red against the old play name
first, then green. The asking seat had not wired the old name. The lead tells it the new one
with the release.

`CCY_VERSION` is bumped 3.124.84 to 3.124.85 and the container 2.84.93 to 2.84.94 (the launcher's
`REQUIRED_CONTAINER_VERSION` and the Dockerfile label), with a `docs/ccy-changelog.md` entry,
because `paths` is an image source.

## Gates run

- `go test ./internal/teamchat/...`, `go vet ./internal/teamchat/...` and `go test ./rules/` (qa
  module): all green except the D 3.1 test above.
- `scripts/test-teamchat-play-jinja.bash`: 56 passed, 0 failed.
- `./scripts/qa-changed.bash` failed on three gates, none of them a test:
  - **ccy-version-bump: real, and the commit needs it.** `paths` is a ccy image source, so
    `CCY_VERSION` (now 3.124.84 in `files/var/local/claude-yolo/claude-yolo`) must be bumped,
    with its comment's "Container" line. The earlier C.2 `paths` changes already needed this.
    It is left undone here: the bump is one shared line that other agents in this worktree also
    edit, and the commit waits on the decision anyway.
  - ansible-syntax: environmental. The container has no vault password file (the second fault
    below).
  - go (go-qa): environmental, and not checkable here. go-qa's pinned ast-grep 0.50.0 is
    amd64-only and this container is arm64, so the analyzers (testclock, realtime_bound,
    project_seam_commands and the rest) ran only through `go vet`'s view of them. CI's amd64 run
    must judge them. A third dogfood fault: go-qa cannot run at all on an arm64 container.

## Open

- Unchecked: whether the host's `lxc` package ships `lxc@.service`. Fedora's is believed to.
  Without it, every start is refused with systemd's "no such unit".
- For C.3: the guest systemd may not be ready when reconcile runs right after a boot start.

## Dogfood faults for the lead to log

- `project_containment` resolved `../untracked/...` against the repository root instead of the
  command's `cd` directory, and denied a write inside the repository (from the previous agent).
- `lint_on_edit` runs `ansible-playbook --syntax-check` and reports the container's missing
  vault password file as a lint failure (`R-LINT-FAILURE`). It did so on a scratch copy before,
  and on `playbooks/imports/play-teamchat.yml` itself in this session. An environmental failure
  is reported as a defect of the file.
