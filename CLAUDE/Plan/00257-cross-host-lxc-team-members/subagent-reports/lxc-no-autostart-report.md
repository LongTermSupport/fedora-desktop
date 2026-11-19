# Task B.12: no LXC member session starts with its container

The owner's order (verbatim): "there is no need for claude to start as soon as the LXC
container boots - in fact that is AWFUL - remove that completely." The coordinator approved
the design. The commits are on branch `plan-xhost-lxc-members`; `git log` has their SHAs.

## What changed

### v1 (`helpers/agent_bus_lxc/members.py`)

- `render_member_unit` writes no `[Install]` section and `Restart=no`. The restart and
  start-limit lines are gone.
- `_ensure_service` never enables or starts the unit.
  - A unit that is `enabled` (made by an earlier run) gets `systemctl disable --now`, which
    removes its wants link and stops it. The run prints `... disabled and stopped: it no longer starts with the container`.
  - A changed unit only gets `daemon-reload` (`updated`), so a session started on demand keeps
    running.
- Tests are in `tests/helpers/agent_bus_lxc/test_members.py`, `ServiceTest`:
  - the placed unit is never enabled or started, and a second run changes nothing;
  - the rendered unit has no `[Install]` or `WantedBy`, and has `Restart=no`;
  - a changed unit is reloaded and its running session left running;
  - a stopped session is left stopped;
  - an enabled, running unit is disabled and stopped.
- RED: five failures against the old code. GREEN: all pass.

### v2 (`src-go/internal/teamchat/host/lxc`)

- `unit/unit.go` renders no `[Install]` and `Restart=no` (the golden file is updated).
  `TestRenderNeverStartsAtBootOrByItself` refuses `[Install]`, `WantedBy=`, `RequiredBy=`,
  `UpheldBy=`, `Alias=`, `Also=` and any `Restart=` other than `no`.
- `apply.go` `placeUnit` writes the file and never makes a wants link. It never calls Start or
  Restart: only Reload, for a new or changed unit, or one not yet recorded joined.
  - A wants link left by an earlier apply is removed, and the seat's session is stopped (Stop,
    then Reload). The line is `unit stopped and its wants link removed: it no longer starts with the container`.
  - `removeUnit` accepts a missing `multi-user.target.wants` directory.
- Tests:
  - `apply_test.go`: `placedUnit` (no link, `Reload` only), the new
    `TestAWantsLinkIsRemovedAndItsSessionStopped`,
    `TestUnitChangedIsReloadedNotStartedAndServiceOffRemoves` and
    `TestUnitReloadFailedIsRetried`;
  - `faults_test.go`: `TestUnreachableSystemdIsTheUnitDown`.
- RED: six failures against the old code. GREEN: all pass.

### On-demand start

- **v1, new in ccy-teams.** An LXC member's row with a handle has the menu line **Start its
  session in its LXC container**. Package `src-go/internal/teams/lxcsession`:

  1. reads the play's member record for the member's container and user (only an entry recorded
     `joined`, or `stopped`);
  2. readies sudo;
  3. checks with `lxc-ls` that the container runs;
  4. runs `lxcfreeze.Lister.StartUnit`, which is `sudo -n lxc-attach ... -- env -i PATH=... systemctl start -- agent-bus-claude-<user>.service`. A plain start works on a unit that is
     not enabled.

  It says the outcome, with the attach command. The help text is in `picker/help.txt`, and
  `teams.Wire` wires it.

  Tests: `lxcsession_test.go` covers a start, a member recorded stopped, a stopped container, a
  record that is deferred or left, and systemd's refusal shown in its own words. There are also
  tests in lxcfreeze `attach_test.go`, picker `lxc_test.go`, `menu_test.go` and `order_test.go`,
  and teams `app_test.go`.

- **v2:** the start that already exists, `teamchat-admin lxc restart --member C/K`
  (ResetFailed, then Restart). Restart starts an inactive unit that is not enabled. The
  `restart_test.go` cases now run over a seat placed with no wants link, and pass.

### Detector for the class (Defence Before Fix)

- **Class:** an LXC member's session unit that starts at boot or at apply.
- **Detector:** the rendering tests in both generations (above), together with the apply tests
  that hold that no enable, start or wants link happens. Both are blocking: the helper tests and
  go-qa run in `qa-changed`, `qa-all` and CI.
- **Why no analyzer or semgrep rule:**
  - The v2 unit text is assembled from non-constant parts (`strings.Join`, path functions).
    The constant-folding approach of the existing `project_unit_no_install` analyzer cannot see
    it.
  - v1 renders in Python. A semgrep pattern on the Python list literal would only match its
    current shape.
- **Conflict with `project_unit_no_install`:** none. That analyzer flags only `Restart=always`
  with no `[Install]`, and these units are `Restart=no`.

### Other places swept

Searched for `WantedBy`, `multi-user`, `enable`, `wants link`, `Restart=always`, the unit names,
and "starts with the container" / "after a reboot". No play, task or `files/` template enables
or starts a member session: both generations do it only in the two code paths above.

Docs updated:

- `docs/agent-bus-lxc-members.md` (`service: true`);
- Plan 00209 `DESIGN-v2-C-host.md` 3.9.4 (text and unit);
- `SPEC-n3.md` SU1 and n3-D3;
- the `paths.GuestUnitWantsLink` comment;
- the `members.py` module docstring.

### Host proof and queue

- `lxc-no-autostart-acceptance.bash` (plan folder, R1-R14 and `_planlib.inc.bash`, mode
  gather) reads `agent_bus_lxc_members` and `teamchat_lxc_members` through Ansible, as the plays
  do. For each running member container it checks:

  - the unit file is present;
  - `systemctl is-enabled` does not say `enabled`;
  - there is no wants link.

  It then asks, per container and with a default of no, whether to reboot it. On yes it runs
  lxc-stop then lxc-start, waits for systemd to report running or degraded, and runs
  `pgrep -u <user> -x claude`. It prints `EVIDENCE` lines. Exit 0 means pass, 1 means fail and 2
  means COULD NOT ESTABLISH.

- **This departs from R5 on purpose.** meta-deploy.bash exports `PLAN_ASSUME_YES=1`, and
  `plan_confirm` would take that as yes and reboot every member container unasked. So the reboot
  question is always put at the terminal (a bounded `[y/N]` read from `/dev/tty`), and with no
  terminal the reboot is skipped. Under meta-deploy the batch waits for that answer.

- `meta-deploy.list` order is now: `play-teamchat.yml` (its description now says it removes v2
  wants links and stops those sessions), `play-claude-yolo.yml`, **`play-agent-bus.yml`** (new:
  disables and stops v1 units), **the acceptance script** (new), then Q15, which stays last.
  `meta-deploy.bash --list` parses it.

## Decisions for the coordinator (not taken)

1. **`lxc.start.auto = 1` is still required for `service: true`,** in v1 (`_check_service`)
   and v2 (`service()`). Its only stated reason was that the session comes back after a host
   reboot, and that is now gone. I kept the check and reworded the v1 message, because dropping
   a check is a change beyond the brief.
2. **ccy-teams has no v2 LXC member rows.** The board does not list v2 teams, so a ccy-teams
   "Start its session" for v2 needs v2 rows first, which is a feature of its own. Today the v2
   on-demand start is `teamchat-admin lxc restart --member` alone.
3. **The plan's Non-Goal "Any v1 change: v1 is feature-frozen"** is overridden here by the
   owner's order, which covers both generations. PLAN.md does not yet say so.
4. **SPEC-n3's offline text names the wrong tool.** `whyUnitDown` reads `offline: unit down; ccy-teams restart starts it`, but ccy-teams restart does not start v2 LXC seats. It is a
   spec'd string, so I left it.
5. **A member whose workflow relied on the auto-start** will be down after the deploy until
   someone starts it from ccy-teams (v1) or `teamchat-admin lxc restart` (v2). That is the
   owner's order, but the coordinator may want to tell the owner.

## The coordinator's extra items (separate commit)

1. **Done:** the `play-teamchat.yml` description in `meta-deploy.list` is rewritten: 1024
   connections, 256 per source by default, lower where `connections_per_source` says so, the new
   `teamchat-tls@.service` with `LimitNOFILE=2112`, and no reclaim. It names the version, read
   from `files/var/local/claude-yolo/claude-yolo`.
2. **Done:** `SPEC-cross-network.md` now says "one source address", and that a client with
   several addresses is not bounded by it.
3. **Not done: `TestT9AFloodAtTheCapHoldsNoSlot`.** Making it deterministic needs a server-side
   point to observe (the flood's connections accepted and held) before the other source dials,
   and a red proof against the pre-fix terminator from history. I ran out of context budget
   before doing it properly, so it is left for a fresh agent.
4. **Done:** `INSTALL-PATH.md:38` (plan 00246) is rewrapped.
5. **Done:** `subagent-reports/b5-fix2-review.md` is committed.

## Tooling faults met (dogfooding; for the coordinator to log)

- **lint_on_edit, analysistest fixtures:** the hook runs `go vet` on fixtures under
  `src-go/qa/rules/testdata/src/fix/...`, which are GOPATH-style analysistest packages, not
  module packages. It reports a false R-LINT-FAILURE ("package fix/internal/... is not in std").
- **ansible_enforcement, text inside a write:** the hook denied a `python3` heredoc and a
  `cat >>` append because the text being written mentioned `systemctl start` or `enable`. It
  judges file content as if it were a command.
- **mkplan.bash `--journal`** takes the plan number only. The dispatch's
  `--journal 00257-cross-host-lxc-team-members` form is refused ("invalid plan number"), and the
  error does not say that the folder name is not accepted.
- **gofumpt** is not on the container's PATH. I formatted one test file with
  `go run mvdan.cc/gofumpt@latest`, which is an unpinned fetch. This is an IaC gap to check.
- **The pre-push hook's gitleaks step:** a `git push origin HEAD` with nothing new to push
  failed with `qa-gitleaks: a ref line git did not write:` followed by an empty line. The
  message does not say what was wrong.

## Version

The comment fix in `src-go/internal/teamchat/paths/member.go` is image source
(`image-source-paths`), so the pre-commit hook required a container bump. It is now ccy
3.122.32, container 2.84.38, with a `docs/ccy-changelog.md` entry. `play-claude-yolo.yml`, which
is already queued, rebuilds the image and installs the new ccy-teams.

## QA

`qa-changed` passed before each commit (logs `untracked/scratch/noauto-qa*.log`). Go:
`go test` passes for `./internal/teams/...`, `./internal/lxcfreeze/...`,
`./internal/teamchat/host/lxc/...` and `./qa/rules`, and the project rules are clean over the
touched packages. Helpers: `test_members` passes.
