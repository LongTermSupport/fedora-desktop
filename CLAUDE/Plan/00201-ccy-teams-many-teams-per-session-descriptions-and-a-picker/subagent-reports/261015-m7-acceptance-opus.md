# Plan 00201 M7 report: the automated two-team acceptance

Task 2.7, automated part. Not run: it is host-only. It passes `bash -n` and `shellcheck -x`, and
`test_m7_check.py` passes (33 tests). Its model is Plan 00161's acceptance.

## Files

- `acceptance.bash`: the bootstrap, the flags, the legs in order, `check_leg`, and
  `plan_on_cleanup m7_teardown_after_stop`.
- `_acceptance-m7.inc.bash`: the legs, the checks and the teardown.
- `m7_check.py` and `test_m7_check.py`: the judgements. `m7_check.py` uses only the standard
  library and imports nothing from the checkout, because its `segment` command runs inside the
  container under the hooks daemon's venv python. The tests read the team file back through
  `teamfile.parse_team_file` and the host_vars through `declare.parse` and `entry_of`.
- `ACCEPTANCE-owner.md`: ten numbered steps for the interactive part. It also tells the owner
  what to check if a run is killed.

## Design decisions

1. **The owner's teams are never touched.** The two throwaway teams are `accept201a` and
   `accept201b`, made by the checkout's `agent-bus-install team`, as the play runs it. They are
   declared only in `<run dir>/host_vars.yml`. ccy-teams and the ccy launches read that file
   through `CCY_TEAMS_HOST_VARS`, which `src-go/internal/teams/config` supports. The run refuses
   to start if the owner's host_vars declare either name.
2. **The Quick Launch record is saved and restored.** Every ccy launch rewrites
   `.claude/ccy/.last-launch.conf`, including `LAST_TEAMS`. 00161 does not restore it; this run
   does, and C1 checks it is byte-identical.
3. **The session is judged inside its container**, through `podman exec`:
   - `pingbus` runs with the environment that `seat exec` gives a session
     (`PINGBUS_HOME=/tmp/pingbus-home`, `PINGBUS_TEAMS=A,B`);
   - the per-session record is read at `/tmp/pingbus-sessions-<uid>/<sid>`;
   - the bus-teams handler is loaded from its file by the daemon's venv python, which is
     resolved with `.claude/hooks-daemon/scripts/lib/resolve_venv.sh python <daemon dir>`. If no
     venv resolves, the check fails and names the cause.
4. **SessionEnd** is checked from inside the container. A watcher started there before the input
   closes polls the record every 20 ms and writes a marker into the run directory, which the
   container mounts under `/workspace`. If the record is still there when the session has ended,
   the check fails.
5. **Routing uses hand-added host members.** `m7peer.1+m7lab.host` is added to both teams and
   `m7solo.1+m7lab.host` to team A only. The validator needs only a role in the team record, not
   room membership.
6. **Firewall rules are checked before and after.** Before, the installer's own
   `firewalld.rules` record for each team is read and each rule must be present, permanent and
   runtime. After the purge, each rule must answer `no`. C1 also checks that no unit, state,
   stamp or seat is left.
7. **Sudo freshness.** Each poll checks `sudo -n true`. A lapse fails the run and the trap tears
   everything down.

## Risks for the first host run

- The first turn needs a `result` line. The project's Stop handlers may make the model go on,
  which costs time but stays bounded at 240 s.
- `ccy-teams --list` must exit 0. It also reads the owner's built teams, which show as
  "decide", since the run's host_vars do not declare them. If those reads exit 1, the table is
  printed and the check fails.
- The 00194 goodbye adds a message at SessionEnd. Nothing here counts messages.
- A plain session is told apart from the owner's sessions as the one new ccy container. A
  concurrent launch by the owner fails the check, as in 00161.
