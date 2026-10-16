# LXC member add: btrfs rootfs, checkout search and completion, context, next steps

Branch `fix/lxc-btrfs-checkouts`, from local F44 `dbfbee3a`. Two commits: the urgent backing-store
fix alone (`c77b9645`), then everything else.

## 1. The bug: a btrfs rootfs

`helpers/agent_bus_lxc/derive.py` `ROOT_READ_PY` (the one host-side reader used by the User-step
home search, the Checkout-step probe and the play's `members.py`) stripped only `dir:` from
`lxc.rootfs.path`. LXC's btrfs store writes `btrfs:<path>`, a subvolume directory on the host.

- First commit: `dir:` and `btrfs:` (and a bare absolute path) are read as host directories. Every
  other store is refused, naming the store and the config line. No Go copy of the parse exists.
  No deploy is needed: ccy-teams fetches the reader from `declare.py` in the checkout at run time.
- Second commit: `overlay`, `overlayfs`, `zfs`, `lvm`, `loop`, `rbd` and `nbd` are read through
  the running container's own root, `/proc/<init pid>/root`, with the pid from `lxc-info -p -H`.
  The pid is asked again once the root is held, so a restarted container is never misread. A
  stopped container on one of these stores is refused, saying it must be running. An unknown
  store is refused, naming it.
- Why not `lxc-attach`: `/proc/<pid>/root` keeps the reader's guarantees (runs nothing in the
  container, follows no link, sees the container's own mounts) and needs no second reader inside
  the container.

## 2. Candidate checkouts: find, not a marker

The home search (`checkouts` mode) is bounded on several axes:

- depth 4, at most 30 checkouts;
- skips hidden trees and `node_modules`, `vendor`, `venv`, `__pycache__` and `site-packages`;
- stops after 5 s and reports `timed_out`, with what it found by then.

Each checkout carries a `ccy` flag (a real `.claude/ccy` directory), and ccy projects sort first.
`declare.py lxc-checkouts` prints `CHECKOUT<TAB>path<TAB>repo<TAB>ccy|-`, plus `TIMEDOUT` when the
search ran out of time. Go also bounds the read (`ReadTimeout`, 20 s) and says a search past it
"took too long and was stopped".

**Recommendation: find. The marker was not built.**

- **It exists when it matters.** A marker exists only after ccy has run as that user in that
  container. lxc-add is the step before that, so on a fresh member there is nothing to read.
- **It is not ours to write.** Inside a member container `ccy` is the client project's
  launcher. A marker would depend on code we do not ship.
- **It goes stale.** A checkout that moves or is deleted leaves a wrong answer, and a user with
  several checkouts needs several entries. find reads the tree as it is now, read-only from the
  host, with every bound above.

## 2b. The Checkout step and Tab completion

The step is a gum choose of the checkouts found (`path   (owner/repo[, a ccy project])`) plus
**Type a path…**, which opens a directory list in the house fzf picker:

- **Prompt and rows.** The prompt is the directory. Rows are its subdirectories (`name/`, with
  `(a git checkout)` where one is), **Use <dir> as the checkout** (first when the directory is a
  checkout), `../` up, and **Back to the checkouts found** (or **Cancel** when none were found).
- **Moving.** Arrows and Enter open a directory or choose. With nothing typed, Tab on a row
  opens it too.
- **Typed text.** Matching is exact (`--exact`), not fuzzy. Tab completes what is typed as a
  shell does, whichever row fzf highlights: the one directory it starts, or the prefix shared
  by several (whole characters only). Absolute paths and hidden names work. "Nothing in X
  starts with Y" is printed when nothing matches. Enter takes the text as the path; a trailing
  `/` opens it instead.
- **Listings.** Each listing is the new `dirs` reader mode, read from the host's side and
  bounded in time. Names that could steer the terminal are left out and counted in the header.
  A listing that fails is printed and the list stays where it was. When the first listing
  fails, the run ends saying why.
- **ui change.** `PickSpec` gained `Typed`, `Prompt` and `Query`, and `Picked` gained `Query`
  (fzf `--print-query`; exit 1 on typed text hands back the text instead of leaving; `q` is not
  bound while typing). Tab goes through `PickSpec.Keys`, a constant `completeKey`, so it moves
  onto `ui.Action{Label: "Tab complete", Key: "tab"}` when dbf/hidden-bindings lands.
- **Tests.** The scripted console (`teamtest.Script`) gained `TYPED:<text>` answers.

## 3. Context on every question

Each step heading reads like `3/4  Checkout · Add an LXC member to <team> · <container>, user <user>`. Each gum and fzf question's header, and the directory list's border title, start with
`Add an LXC member to <team> · step 3/4 Checkout · <container>, user <user>`.

## Coordinator additions (a) to (e)

- **(a)** The end now names the kit's `agent-bus-claude`. A project launcher that supports it
  (`ccy --teams` with no value) is mentioned only as an alternative. `usage.txt`, the docs and
  the test pins were updated.
- **(b) Already-running session.** lxc-add looks for a Claude session already running. It uses
  the reader's new `sessions` mode: the container's own `/proc` read from the host through its
  init pid, giving that uid's `claude` processes (or `node`/`bun` running it) and their start
  times. It prints one of:
  - "A Claude session for U is already running in C (started …). End it and start it again
    with agent-bus-claude, as U in <checkout>, to join T."
  - how to start one, when none is running;
  - a failed look, with what to do either way.
- **(b) Why not `lxc-attach … ps`.** The Go project rule forbids it: `ps` is owned by
  `internal/sessprobe`. The `/proc` route also runs nothing in the container.
- **(c) The pingbus refusal, option 2.** When `PINGBUS_TEAMS` is unset and the member env file
  (`${PINGBUS_ENV:-${XDG_CONFIG_HOME:-$HOME/.config}/pingbus/env}`, the launcher's own lookup)
  exists, the refusal names it and gives `set -a; . <file>; set +a`. pingbus does not read the
  file itself, for two reasons:
  - the file is shell, sourced by `agent-bus-claude`, and a second parser could disagree with it;
  - an automatic read would silently make every shell with that file active.
- **(d)** The new Check line: `set -a; . ~/.config/pingbus/env; set +a; pingbus config check && pingbus history --limit 1`. It reads the team's room live as the member.
- **(e)** The handle is shown as `<handle> (container C, user U)` in the summary and in the
  result. The GitHub line now says `agent-bus-claude` refuses to start until gh is logged in
  (the kit's real precondition, README.lxc) and gives `gh auth login --hostname github.com`.

## Versions, deploy, QA

- **Versions.** CCY 3.99.12.5 and container 2.77.4 (`helpers/pingbus` and `helpers/agent_bus`
  are baked into the image), after F44 (`1d3c65bb`, 3.99.12.4 and 2.77.3) was merged into the
  branch. The changelog section sits above F44's. lxc-add's takeover offer carries the Role
  context line too.
- **After Fable's review.** The attach hints carry `-P <lxcpath>` (read once through the new
  `lxcfreeze.Lister.LxcPath`). An `lxc-info` that never answers is refused naming it, not a
  traceback. The session start times use the host's boot time.
- **Deploy.** The existing `play-claude-yolo.yml` entry in `meta-deploy.list` keeps its place;
  its comment now describes this change. LXC members get the new pingbus message from the kit
  on the next `play-agent-bus.yml` (any team apply).
- **QA.** `qa-changed` is green. The targeted suites pass:
  - Go: ui, teamtest, lxcadd, picker, declare, lxcfreeze, and all of `teams/...`;
  - unittest: `test_derive`, `test_members`, `test_declare`, `test_config`;
  - the shell suites `qa-changed` ran.
- **A pre-existing defect in `scripts/test-ccy-lifecycle.bash`.** Inside a bus session it
  inherited `PINGBUS_MEMBERSHIP`, so three no-option wrapper cases failed. The fix unsets it in
  `wrapper_case`.
- **Not done.** `qa-all` was not run, as instructed, and qa-reviewer was not run.
