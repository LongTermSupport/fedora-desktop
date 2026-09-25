# Agent mailbox: the ccy container ⇄ a desktop `cc` agent

A file mailbox lets two agents that share one checkout pass work between them:

- **The ccy agent** works inside a container. It edits and commits, but it must never run
  Ansible or anything else on the host.
- **A desktop `cc` agent** runs on the host itself, so it can run the plan scripts there.

It is dormant tooling: nothing starts it. Use it when the owner sets up a desktop agent to
run host steps for a ccy session; otherwise `meta-deploy.bash` is how the owner is asked to
run things.

The mailbox lives at `untracked/agent-mailbox/` (or `AGENT_MAILBOX_DIR`). Its messages can
hold host details, so it is never tracked. The watcher makes it, mode 0700, on first use.
The watcher and its test are tracked:
[`scripts/agent-mailbox-watch.bash`](../scripts/agent-mailbox-watch.bash) and
[`scripts/test-agent-mailbox-watch.bash`](../scripts/test-agent-mailbox-watch.bash).

## Folders

- `to-desktop/`: requests from the ccy agent, named `NNNN-<slug>.md`.
- `from-desktop/`: replies, named `NNNN-reply.md`, where `NNNN` matches the request.
- `to-ccy/`: messages the desktop agent starts, named `D-NNNN-<slug>.md`. Use it for a
  question, a problem, or something the ccy agent should know, when no request is open.
  The ccy agent answers in `to-desktop/`, naming the `D-NNNN` it is replying to.

## What the desktop agent may run from a request

Only the following:

- `./CLAUDE/Plan/meta-deploy.bash`;
- a plan's own `triage.bash`, `deploy.bash` or `acceptance.bash` under `CLAUDE/Plan/`;
- read-only diagnostics, such as `systemctl status`, `journalctl` or `git log`, always
  with `--no-pager` or `| cat`.

Anything else goes to the owner first. That includes rebooting, logging out, changing a
file by hand, running a playbook directly, or any `git` command that writes. The rule that
every system change goes through Ansible applies here as it does everywhere.

## Handling a request

1. Before starting, write `from-desktop/NNNN-reply.md` with its first line set to
   `STATE: running`.

2. Do exactly what the request says, in the order it says.

3. Finish the reply with these lines, then anything worth noting:

   - `STATE: done` or `STATE: failed`;
   - `EXIT: <exit code>`;
   - `CAPTURE: <path>`, the run's `untracked/plan-runs/...` directory, if it made one.

   Don't paste logs. The ccy agent reads the capture itself.

4. Never run `git pull`, commit or push: both agents share this checkout.

## Staying on watch

A request can arrive at any time, including seconds after the reply to the last one, so a
watcher is armed whenever an agent is not handling mail:

- Start it as a background command, not in the foreground:

  ```bash
  ./scripts/agent-mailbox-watch.bash desktop
  ```

  It checks every 5 seconds, and exits as soon as a request in `to-desktop/` has no reply,
  printing the waiting request's path. The harness wakes the agent when a background
  command exits. Handle the request, then start the watcher again. If it exits 3 (six
  hours with nothing), just start it again. Stay on watch until the owner says to stop.

- The ccy agent runs `./scripts/agent-mailbox-watch.bash ccy` in the same way. That wakes
  it when a reply reaches `STATE: done` or `failed`, or when the desktop writes to
  `to-ccy/`.

- Start the watcher again straight after writing a reply, and before stopping for any
  reason. If a request is already waiting, it exits at once.

- Take requests one at a time, lowest number first. Two Ansible runs must never overlap.

- A request can say it must wait for an earlier one. Honour that.

## Tearing it down

Stop both watchers (`TaskStop` on the background command), then remove
`untracked/agent-mailbox/`. The tracked watcher needs no change; the next use makes a
fresh mailbox.
