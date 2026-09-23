# PR #47 review findings ported to F44 (Plan 00135 Task 3.8)

PR #47 built session restore on a design F44 replaced (Plan 00135), so it is not merged.
A comparison of the two found gaps in F44's own code. This report records what was ported
and how each item was proven. Every test was written first and seen failing.

## 1. `ccy-sessions reboot` withdraws a partial warning

- **Defect:** `reached="$(signal_projects …)" || return 1`. If project A was warned and
  project B then refused, the command returned 1 with no `reboot-cancelled`, and A waited
  for a reboot that never came. The trap covered INT/TERM only.
- **Fix:** `withdraw_unless_rebooting`, an EXIT trap armed before the first warning, in
  the same shape as `shutdown-with-update`'s `withdraw_unless_down`. It withdraws on any
  non-zero exit: a refused warning, a refused one-minute warning, Ctrl-C (130), or
  `systemctl reboot` failing. `cancel_reboot` now only exits 130 and leaves the withdrawal
  to the trap, so there is one path.
- **Withdrawal carries on.** `signal_projects` still stops at the first refusal for a
  warning. For `reboot-cancelled` it continues through every project and fails at the
  end. A withdrawal that fails for some projects says "some still expect the reboot".
- **The reboot itself.** INT and TERM are ignored while `systemctl reboot` runs. A probe
  showed bash runs an EXIT trap when an untrapped TERM kills it, so the shutdown reaching
  the tool would otherwise withdraw a reboot that is going ahead.
- **Tests** (`scripts/test-ccy-sessions-reboot.bash`, 129 pass; 5 failed before the fix):
  - the first warning fails at B: exit 1, no reboot, A is told `reboot-cancelled`;
  - the withdrawal also fails at A: B is still told, and the output says so;
  - `systemctl` refuses: both projects are withdrawn;
  - a reboot that goes ahead withdraws nothing.
    The CLI stub gained `TEST_CLI_FAIL_MATCH_2`, for a second failing call.

### The daemon's "no live session" refusal stays a failure

`hooks-daemon signal --all-sessions` exits 1 when `discover_session_ids` finds no
`<session>.json` under the project's signal directory (`daemon/cli.py` `cmd_signal`,
`utils/operator_signal.py`). That function counts every file present and never checks
liveness. So the refusal means no daemon-tracked session has ever written a sidecar in
that project. It does not mean "the session is idle".

The refusal cannot tell "no agent to warn" from "a live agent the daemon is not
tracking", and the wrong guess reboots a working agent unwarned. So it stays a refusal,
as fail-fast requires. The cost: a project in that state blocks `ccy-sessions reboot`
until its session is ended or the daemon runs there. Plan 00137's cycle then exits 22
and alerts, which is its intended outcome. With item 1 fixed, the projects warned before
it are cancelled cleanly.

## 2. Every launcher flag has a replay decision

- **Change:** `ccy_registry_replay_args`' inline case now reads from four named groups
  (`CCY_REGISTRY_{DROP,DROP_VALUE,KEEP_VALUE,KEEP}_FLAGS`) through
  `ccy_registry_flag_class`. Behaviour is unchanged for every existing flag, and all
  earlier replay cases pass.
  - `--help`, `--version`, `-h` and `-v` are classified as drop, so the population is
    complete. They exit before a session exists.
- **Test** (`scripts/test-ccy-session-registry.bash`): the flags are derived from both of
  the launcher's parse sites. The argument loop gives 25, and the `"$1"` checks give 4.
  The test fails on:
  - a flag in no group;
  - a flag in two groups;
  - a classified flag the launcher does not parse.
    Each bucket has a floor and is printed, so a parse site that stopped matching cannot
    pass by shrinking.

## 3. Every prompt a launch can wait at is registered

- **Test:** every `read … -p` in the launcher and `lib/*.bash` (48 sites, 10 files) must
  print a `CCY_PROMPT_*` constant that `ccy_known_prompts` lists, or be in
  `UNREACHABLE_PROMPTS` with its reason. The reasons are:
  - `--debug`, `--custom`, `--custom-docker`, `--top` or `--export-token`, all dropped on
    replay;
  - a sub-prompt of a listed menu;
  - token creation, which is reached only past a listed prompt or a dropped flag;
  - `$prompt_text`, which is built from `CCY_PROMPT_SSH_KEY`.
    An allowlist entry that names no real prompt fails, and so does an interactive `read`
    with no `-p`.
- **It failed on the two gaps:**
  - "Stop compose services?" (`claude-yolo`), which is reachable after claude exits in a
    restored session;
  - the token-setup pause (`token-management.bash`), an `echo` above a bare `read -r`.
- **Fix:** `CCY_PROMPT_COMPOSE_STOP` and `CCY_PROMPT_TOKEN_SETUP` are registered as
  `compose-stop` and `token-setup`. The token-setup pause is now
  `read -rp "$CCY_PROMPT_TOKEN_SETUP " _unused`.

## 4. An unlisted registry is not an empty one

A registry path that exists but is not a readable, searchable directory now fails the
restore. Before, the glob found nothing and the restore reported "nothing to restore",
exit 0.

- **Tested** with a regular file at the registry path.
- **Not tested:** an unreadable directory. The container runs as root, and root reads any
  directory, so it cannot be staged here.
- **Unchanged:** an absent registry is still a clean no-op.

## Version

CCY 3.62.0, token-management 1.13.0, with a `docs/ccy-changelog.md` entry.

## Not ported

- **Item 5 of the triage** (records left from before the opt-in) and **item 7**
  (`restore-status`): these are features, not defects.
- **Item 8** (the acceptance template): it needs adapting to F44's output.
- **Item 4** (`.claude/.gitignore`): landed on F44 separately.
