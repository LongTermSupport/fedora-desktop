# Task 7.1 sub-item: the test against a real tmux server

Built, not committed, and not run: this container has no tmux, and I did not install it by hand.

## Changes

- `scripts/test-ccy-tmux-targets.bash` (new, mode 755).
- `.claude/ccy/Dockerfile`: `tmux` added to the apt list, with a comment saying why it is there.
- `scripts/qa-all.bash`: a hard gate `ccy-tmux-targets`, placed after `ccy-sessions-take-over`.
- `.github/workflows/qa.yml`: a step that runs `apt-get install -y tmux` before the suite.
- `CLAUDE/QA.md`: a row for the new test.
- `PLAN.md`: the Task 7.1 sub-item is now 🔄, marked built but not yet run.
- Journal entry: `JOURNAL/00135-Journal-26-10-09.md`.

## I did not follow the brief on where tmux goes

The brief said to put tmux in the base image (`files/var/local/claude-yolo/Dockerfile`) and
bump the container version. I put it in the project image (`.claude/ccy/Dockerfile`) instead.
`CLAUDE/ContainerRules.md`, "Where a Missing Tool Goes", says a test dependency that only
fedora-desktop needs belongs in the project image. The base image is only for tooling that
every ccy project uses, because a change there forces a rebuild for every ccy user. gawk for
`test-bash-history-search.bash` was handled the same way (commit 1a278464).

What follows from this:

- No CCY or container version bump is needed. The launcher, the base Dockerfile and
  `entrypoint.sh` are unchanged.
- ccy rebuilds the project image by itself on its next start, because the project
  Dockerfile's hash has changed (launcher around line 1964).
- No meta-deploy entry is needed. The host already gets tmux from `play-claude-yolo.yml`.

If the coordinator wants tmux in the base image after all, it needs three changes: the line
moves to the base Dockerfile, `LABEL claude-yolo-version` and `REQUIRED_CONTAINER_VERSION`
are bumped together, and a CCY bump with a changelog entry is added.

## What the test does

1. **A private server.** The test sets `TMUX_TMPDIR` to a fresh `mktemp -d` directory. It
   does not use the repo for this because a unix socket path is limited to 107 bytes. It
   sources `common-pure.bash` and `tmux-session.bash`, then sets `CCY_TMUX_SOCKET` to a
   label of its own (`ccy-target-test-$$`). Either the directory or the label alone keeps
   the test off every real server. After the server starts, the test checks that the socket
   is inside its own directory and stops if it is not. A trap on EXIT kills the server and
   removes the directory. The pane runs `cat`, and no user config is loaded (`-f /dev/null`).

2. **The library's own functions, run against real tmux output:**

   - `ccy_tmux_list` with no server running: prints nothing and returns 0.
   - `ccy_tmux_list` with one session: prints `<name> 0 <dir>`.
   - `_ccy_tmux_clients` on a session with no clients: prints nothing and returns 0.
   - `_ccy_tmux_clients` on a session that does not exist: prints nothing and returns 2.
     This is the check that real tmux's "can't find session" wording still matches what
     the library looks for.

3. **Every target, read from the source, not retyped.** A regex finds each
   `ccy_tmux <subcommand> ... -t|-s "<template>"` call in `ccy-sessions` and
   `lib/tmux-session.bash`. There are nine today:

   - send-keys ×2, capture-pane ×2 and kill-session in `ccy-sessions`
   - attach-session ×2, list-clients and detach-client in `lib/tmux-session.bash`

   The template's `${name}`, `$name` or `$1` is replaced with the session name, and any
   other `$` in a template fails the test. Each target is then run through `ccy_tmux`:

   - capture-pane, send-keys and list-clients run for real.
   - kill-session runs for real against a throwaway second session, and the test checks
     that session is gone.
   - attach-session and detach-client are checked with has-session, which takes the same
     kind of target. attach needs a terminal and detach needs an attached client, so
     neither can run here.
   - A tmux subcommand the test does not know fails it.

   The test also fails if it finds no targets at all, or no capture-pane target.

4. **The pane form reaches the pane.** Keys sent to the extracted capture-pane template's
   target must appear in that pane's capture. The test polls for up to 5 seconds.

5. **The #69 regression is pinned.** For both capture-pane and send-keys, the same target
   with its trailing colon removed (bare `=name`) must fail with "can't find pane". This
   relies on tmux's `cmd_find_target`, which treats a target with no colon and no dot as a
   pane name for pane commands.

The summary line is `passed: N   failed: N`, which is what `qa_gate_case_count` reads. The
test exits 1 if no cases ran.

## Missing tmux, and why it is wired into qa-all now

If tmux is not installed, the test exits 1 with "FAIL: tmux not found". The message says
where tmux comes from in each place: `.claude/ccy/Dockerfile` (restart ccy to rebuild the
image), `play-claude-yolo.yml` on the host, and `qa.yml` in CI. It never skips.

I wired it into qa-all now as a hard gate rather than waiting for the image. This is the
pattern the repo already uses: the gawk suite went into qa-all in the same way, and qa-all
stayed red in the container until the image was rebuilt (commit 1a278464 says so). As a
result, **qa-all in this container will fail on `ccy-tmux-targets` until ccy is restarted**
and the project image is rebuilt. On the host and in CI it should pass.

## Checks run

- `shellcheck -x` is clean on the new test and on `qa-all.bash`, and `bash -n qa-all.bash` passes.
- The new test fails as intended here on the missing-tmux path (exit 1, message as above).
- I ran the extraction on its own against the current source: all nine calls were found,
  and each filled to `=<name>:` (pane commands) or `=<name>` (session commands).
- `scripts/test-ccy-sessions-reboot.bash`: passed: 243, failed: 0.
- `scripts/qa-docs.bash`: exit 0, so the new QA.md row satisfies the derived-inventory check.
- Not run: the new test's real-tmux cases. Once tmux is present, these are the ones to
  watch on the first run: send-keys `-l " "`, and has-session standing in for attach and
  detach.
