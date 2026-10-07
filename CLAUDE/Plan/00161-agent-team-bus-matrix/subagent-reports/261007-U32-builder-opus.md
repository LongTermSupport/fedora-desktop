# U32 builder report: host seat commands for a ccy checkout

Branch `seats/U32`, from `origin/seats-integration` (after U29, U30 and U33).

## What was built

- `helpers/agent_bus/checkout.py`, new. Pure parts: `role_from_env_local` (the
  `HOOKS_DAEMON_HOSTNAME` that `ccy.env.local` assigns, by parsing), `seat_host` (that role
  normalised by `registry.resolve_host`, else `local`, D49), `parse_bus_address`
  (`agentbus0`'s one non-link address from `ip -j addr`), `take_actions` and
  `remove_actions` (the `add-member` / `park-member` calls an observed state needs). The
  executor: `System` (cwd, uid, and the host side: `systemctl is-active agent-bus-hs@<team>.service`, the bus address, `sudo [-n] /usr/local/bin/agent-bus ...`,
  a clock), and `check`, `take`, `seat_list`, `remove`.
- `helpers/agent_bus/cli.py`: `seat check LIST`, `seat take LIST [--no-prompt]`,
  `seat list`, `seat remove LIST`; exit 64 for a malformed list (`seat.SeatListError`),
  75 for a held seat (`inbox.Busy`, new `EXIT_BUSY`), 78 for `CheckoutError`,
  `config.ConfigError` and `inbox.StateError`. `main` takes `checkout_system=` for tests.
- `files/usr/local/bin/agent-bus`: `seat` runs as the caller, in the caller's directory
  (`run_user_tool`: `exec /usr/bin/python3 -I "$AGENT_BUS_PYZ" "$@"`, no `runuser`, no
  `cd /`), and is refused as root (77); every other command is unchanged and still refused
  without root. Usage lines name the four seat commands.
- Docs: `docs/agent-bus.md` (a row in the commands table, a new "A ccy session: seats"
  section under "Joining a team", the podman row's bundle path, "Starting sessions", and
  `seat remove` in "Running the team"); `files/opt/claude-yolo/optional/agent-bus/README.podman`
  rewritten; `admin.NEXT_STEPS["podman"]` (the bundle README) rewritten. U19's
  `ccy.env.local` opt-in text is gone from all three.

## How each command behaves

- `check`: parse (64), each team's homeserver active here (78), print the canonical list.
  Needs no checkout; changes nothing; no sudo.
- `take`: as `check`; then the checkout (git top level of the cwd, owned by the user, with a
  real `.claude/ccy/`), the seats' tree (`pingbus/`, `seats/`, `seats/<team>/`: each, when
  present, a real directory of the user), `git check-ignore -q .claude/ccy/pingbus/`; then
  each named seat observed (directory, lock, `member.json` handle). Any held: 75, nothing
  done. A directory without `member.json`: 78, naming `seat remove`. For each seat with no
  directory: the repo (`registry.repo_from_remote` on `origin`), the `<host>` and the bus
  address are read only now, the tree made 0700, the handle said on stderr, one
  `sudo agent-bus add-member <team> --repo --seat --host --type=podman --role=worker --address --out=<seat dir>` (`sudo -n` with `--no-prompt`), and a `CHANGED` line. A
  failing call stops it with 78 (with `--no-prompt`, naming `sudo -v`). A second identical
  take reads nothing from the host and prints nothing.
- `list`: `seat.seat_lines(root, None)`; an unreadable seat is said on stderr, the others
  still print, exit 78.
- `remove`: as `check`, the checkout and tree, observe; any held: 75. Each present seat's
  lock is taken (closing the race with a launch), `sudo agent-bus park-member <team> <handle>` run, the directory deleted, a `CHANGED` line; then `seats/<team>/`, `seats/`
  and `pingbus/` removed when empty. A seat with no directory parks the handle the
  checkout's rule builds, which is how section 5.5's "a lost bundle is recovered by `seat remove`, then launch again" can work.

## Decision recorded

D58 (DESIGN.md; written as D56 on `seats/U32`, renumbered on integration because U31 took
D56-D57): the edges section 5.6 leaves open, listed above: `list` takes no list; a
directory without a bundle refused; the symlinked-tree refusal; the lock held during
remove; the rule's handle for a lost seat; `CHANGED` in the admin tool's tab form
(`CHANGED<TAB>seat <seat>@<team> <handle>`, where section 5.6's prose shows spaces);
`ccy.env.local` read only as literal assignments, an empty value meaning no role, anything
else naming the variable refused; 77 for a seat command as root; sudo calls the wrapper by
path.

## Verification (in the container)

- `tests.helpers.agent_bus.test_checkout` (new): 44 tests, seen failing (module missing),
  then passing. Covers the row: role parsing and normalisation, `local` without a file or
  an assignment, refusals for twice, symlinked, foreign-owned, and shell-only forms (a
  `touch` line in the file is never run); the calls for new, existing, parked (returns by
  the same call) and held seats, only for the named seats, none when one is held; a team
  not running here (78) and a malformed list (64) by `check`, `take` and `remove` alike;
  `remove` leaving no `pingbus/`; a second identical `take` creating nothing; the git
  ignore check; a symlinked tree; a subdirectory cwd; no bus address refused before sudo.
- `tests.helpers.agent_bus.test_wrapper`: 4 new tests (seat commands run as the caller in
  place, refused as root with 77, the user tool has no privilege change, usage lines), seen
  failing, then passing; the existing `test_refuses_without_root` still holds for admin
  commands.
- `tests.helpers.agent_bus.test_member_docs`: new `CcySeatsTest` (opt-in gone, `ccy --teams`, the seats layout, one seat per team, `pingbus history`, `local`, `seat list`
  and `seat remove`, "as yourself"), seen failing on the old guide, then passing; every
  `agent-bus` invocation in the docs parses with the new `seat` parser.
- Every `tests.helpers.agent_bus` module plus `tests.helpers.pingbus.test_bundle`,
  `test_seat`, `test_plugin_contract`: 384 tests, OK.
- `scripts/test-agent-bus-install.bash` (it installs the wrapper): 261 passed, 0 failed.
- `ruff check` (0.16.8) on `helpers/agent_bus/` and the touched tests: clean.
- `shellcheck -x files/usr/local/bin/agent-bus`: clean.

## For the coordinator

- No change under `files/var/local/claude-yolo/`: no CCY or container bump. The code reaches
  the host in the `agent-bus` zipapp the next time `agent-bus-install software` runs
  (`play-agent-bus.yml`, already in `deploy.bash`); no host run belongs to U32 alone, U20
  exercises it.
- U31 calls `agent-bus seat check <list>` (stdout: the canonical list) and `agent-bus seat take <canonical list> [--no-prompt]`, passing on their exit codes (64, 75, 78; 77 if run
  as root). `seat take` prints `CHANGED` lines on stdout: the launcher should send them to
  stderr or ignore them, not to `claude`.
- `docs/ccy.md` still describes the `PINGBUS_TEAMS` opt-in (its CAN-reach row and the
  entrypoint paragraph); that file is U31's per its row, so U32 left it.
- Residual risk, not U32's to fix: the root wrapper creates `--out` with `install -d` as
  root. `seat take` refuses a symlinked tree before calling it, but a session in the same
  checkout could swap `.claude/ccy/pingbus` for a symlink between that check and the
  wrapper's `install -d`, steering a new user-owned directory (holding `member.json`,
  `token`, `README`) to a path of its choosing. Placing the bundle as `$SUDO_UID` (for
  example `setpriv --reuid="$SUDO_UID"` around `install`) would close it; that changes
  U15's wrapper and its tests, so it is left for a review fix.
