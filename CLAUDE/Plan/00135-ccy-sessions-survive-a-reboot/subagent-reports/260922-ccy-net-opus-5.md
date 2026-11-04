# ccy-sessions: show each session's container network

Commit `6674be8c` on `F44`, pushed. `CCY_VERSION` 3.58.3 → 3.59.0.

## What the picker shows now

`ccy_tmux_row` gained an optional fourth argument, so a row is

```
<session name>  <state>  <network>  <directory>
```

`ccy-sessions` passes the network; `ccy_tmux_offer` (ccy's own "a session is detached here"
picker) still passes three arguments and its rows are unchanged — the column is **omitted**,
not padded blank, because a blank column in a picker that never asked would read as
"this session has no network".

## Where the value comes from, and why

The network is a property of the **running container**, and the link from a tmux session to
its container is the **process tree**: the engine client that runs the container is a
descendant of the session's pane, and it carries `--name <container>` on its own command
line. `lib/docker-health.bash:50` already matches containers that way, so this is an
established fact about the launcher, not a new assumption.

Name arithmetic was rejected: sessions are numbered `ccy-<project>`, `ccy-<project>-2`
(`ccy_tmux_next_name`) and containers `<project>_yolo`, `<project>_yolo_1`
(`get_next_container_name`), allocated independently against different occupancy sets. There
is no shape that pairs them, and a picker that guesses wrong labels a session with another
session's network — worse than showing nothing, because a reader would act on it.

Having the container name, the **network itself is asked of the engine**:

```
<engine> ps --filter label=ccy=true --format '{{.Names}} {{.Networks}}'
```

Not `.last-launch.conf`, and not the `--network` flag sitting on that same command line.
Both of those record what was **requested at launch**, while `ccy --connect <net>` attaches
a network to a container that is already running (`lib/network-management.bash`,
`claude-yolo:873-877`). Only the engine knows what a session is connected to now.

The engine binary is not read from config either — it is the basename of the client process
that is actually running the container. That avoids duplicating
`${CCY_CONTAINER_ENGINE:-podman}` outside `common.bash` (which `ccy-sessions` deliberately
does not source: `common-pure.bash:7-16` and the comment at `common.bash:6-12` explain that
`common.bash` exits at file scope when no engine is on PATH, which is why the host-side tools
load only the pure half). It also gives the right answer if two sessions ever ran on
different engines.

## The displayed states

| Word           | Means                                                                      |
| -------------- | -------------------------------------------------------------------------- |
| `<name>`       | the network(s) the engine reports, comma-joined                            |
| `none`         | container running, on no named network — `--no-network`, or a bare default |
| `no container` | nothing to ask about: a `cc` host session, or a container that has exited  |
| `unknown`      | a probe failed; the reason is on stderr above the list                     |

`none` and `no container` are deliberately different words: "connected to nothing" and
"there is nothing to ask" are different facts about a session. `unknown` is the fourth
because a failed probe must not be able to masquerade as either.

On `--no-network` the launcher leaves `NETWORK_FLAG` empty (`claude-yolo:2134-2137`,
`2767-2770`), so the container gets rootless default networking and the engine reports no
named network → `none`. With podman the ordinary default is the **named** network `podman`,
so that case shows as `podman`, which is honest rather than a special case.

## Cost

Flat in the session count, which matters because `rows()` runs on every picker refresh:

- one `tmux list-panes -a`
- one `ps -ww -eo pid=,ppid=,args=`
- one `<engine> ps` **per engine that is actually running a CCY container** — zero calls when
  no session has one

No per-row shell-out. This is asserted by the test, not just claimed.

## Verification — and what was NOT verified

- `scripts/test-ccy-session-network.bash`, 39 cases, green. It covers the pure joiners
  (`ccy_session_containers`, `ccy_network_word`) and drives the real
  `ccy_tmux_network_rows` with `tmux`, `ps` and `podman` replaced by shell functions —
  including an assertion that the stubs really shadow the outside world, so the section
  cannot silently start testing the host.
- Written red first: the run before the implementation failed with
  "ccy_session_containers is not defined".
- `shellcheck -x` clean on all three changed bash files.
- **Not tested against real podman.** This is the CCY container; there is no tmux server on
  the `ccy` socket and no podman here. The exact rendering of `{{.Networks}}` by the host's
  podman is covered by the parser handling both a Go slice (`[a b]`, `[]`) and a comma
  string, but the live command has not been run.

Two states found only by driving that path, both now covered by a case that fails without
the fix:

- **No tmux server at all.** `ccy-sessions` calls the probe before it discovers there are no
  sessions, so the first-run state would have printed a scary error to a user with nothing
  running. Handled the way `ccy_tmux_list` already handles it — no server means no sessions,
  not a failure.
- **No sessions in the table.** A here-string over empty text still yields one blank line,
  which collected an empty engine name that was then RUN as a command.

## Adjacent fix

`ccy-sessions --help` printed `t -euo pipefail` as its last line: `usage()` printed a fixed
line range that included the first line of code, and would have clipped the text as the
header grew — which this change grows. It now stops at the first non-comment line.

## Ordering

Self-contained and additive. Plan 00135 Phase 3's subcommand dispatcher touches the arg
parser at the top of `ccy-sessions`; nothing here goes near it.
