# Plan 00209 Task 3.1: what each v1 hook event costs in Python

Measured before the Go port (SPEC-pingbus-hooks.md), so the port's gain can be stated against a
number. All figures are seconds per run of one hook process.

## Method

- The zipapp was built from this worktree with
  `python3 -m helpers.pingbus.bundle --source <worktree> --out <scratch>/pingbus.pyz pingbus` and
  run as `/usr/bin/python3 <scratch>/pingbus.pyz hook <event>`, as the kit runs it (its shebang
  is `/usr/bin/python3`).
- The world (a scratch directory under `untracked/scratch/`, not tracked): a `PINGBUS_HOME` with
  two teams, `alpha` and `beta`, each a plain bundle (`member.json`, `token` 0600), a
  `state/team.json` cached record (one human, this worker, one coordinator, an org), an
  `about.json`, and three accepted human messages in `state/inbox/`; `PINGBUS_TEAMS=alpha,beta`;
  `PINGBUS_SESSIONS_DIR` in the world; the session socket pair `CLAUDE_CODE_MESSAGING_SOCKET`
  and `CLAUDE_CODE_MESSAGING_TOKEN` set (no socket file: nothing is delivered); a fake `pingbus`
  first on `PATH` (`exit 0`), so `session-start` starts a "watcher" every run as it would with no
  waker; `env -i` otherwise.
- The payload on stdin: `{"session_id","transcript_path","source":"startup"}` for
  `session-start`, `prompt` and `post-tool`; `{"session_id","stop_hook_active":false}` for
  `stop`; `{"session_id","reason":"clear"}` for `session-end`; `reason` `prompt_input_exit` for
  `session-end-exit` (no seat links in this world, so no goodbye is posted and no network is
  used); `conversation` ran with `PINGBUS_MEMBERSHIP` pointing at a desired file of kind `cc`, so
  it wrote `conversation.json`.
- What the runs did: `session-start` gave 7 lines (teams, watcher started, the frame, two
  descriptions, two worker role lines, 6 pending) and wrote the sessions record, the spawned and
  transcript marks, `told-roles.json` and `told.lock`; `prompt` gave the pending line; the first
  `stop` blocked on the pending set and every later one did not (the guard remembered the
  digest), which is the common Stop; `post-tool` answered `{}` (no relay).
- Timing: bash `time` (`TIMEFORMAT='%R %U %S'`) around each run, 20 runs per event, run twice;
  CPU is user + sys of the process and its children. The floor is `python3 -c pass`; `version`
  is `pingbus.pyz version`, the import of the whole CLI with no hook work.
- `/usr/bin/time` is not in the container; nothing was installed.

## Machine conditions

A ccy container on a 22-core host, shared with other agents' builds and tests. Load average
(1, 5, 15 min): 36.2, 21.9, 16.7 just before; 44.9, 32.5, 21.6 at the start of run 1; 23.2, 28.6,
21.4 between the runs; 15.8, 25.0, 20.7 at the end of run 2. The figures are therefore noisy (the
same event moves by up to 2x between runs) and are upper bounds for an idle machine; the medians
and the minima are the useful numbers. Python 3.11.2, `unicodedata` 14.0.0.

## Results

Run 1 (load about 45 falling to 27):

| Event              | wall median | wall min | CPU median | CPU min |
| ------------------ | ----------- | -------- | ---------- | ------- |
| `python3 -c pass`  | 0.044       | 0.028    | 0.029      | 0.025   |
| `version` (import) | 0.433       | 0.277    | 0.401      | 0.272   |
| session-start      | 0.495       | 0.253    | 0.490      | 0.250   |
| prompt             | 0.673       | 0.482    | 0.663      | 0.477   |
| stop               | 0.546       | 0.365    | 0.540      | 0.362   |
| session-end clear  | 0.538       | 0.411    | 0.532      | 0.406   |
| session-end exit   | 0.488       | 0.338    | 0.481      | 0.335   |
| post-tool          | 0.714       | 0.500    | 0.684      | 0.490   |
| conversation       | 0.631       | 0.344    | 0.605      | 0.339   |

Run 2 (load about 23 falling to 16):

| Event              | wall median | wall min | CPU median | CPU min |
| ------------------ | ----------- | -------- | ---------- | ------- |
| `python3 -c pass`  | 0.013       | 0.012    | 0.013      | 0.011   |
| `version` (import) | 0.286       | 0.196    | 0.282      | 0.195   |
| session-start      | 0.260       | 0.214    | 0.252      | 0.213   |
| prompt             | 0.447       | 0.208    | 0.443      | 0.206   |
| stop               | 0.611       | 0.439    | 0.601      | 0.435   |
| session-end clear  | 0.641       | 0.456    | 0.632      | 0.452   |
| session-end exit   | 0.586       | 0.423    | 0.575      | 0.418   |
| post-tool          | 0.536       | 0.341    | 0.530      | 0.339   |
| conversation       | 0.450       | 0.335    | 0.443      | 0.321   |

`python3 -X importtime` on `hook prompt`, three runs: `helpers.pingbus.cli` cumulative import
0.304, 0.306 and 0.369 s.

## Reading

- Every event costs about 0.2 to 0.65 s of CPU, and nearly all of it is the same thing: importing
  the CLI (all 17 modules), 0.2 to 0.37 s, against a 0.01 to 0.03 s interpreter floor. The hook
  work on top (bundle and token checks, two `team.json` parses, re-validating six inbox items, the
  lock probes, the marks) is a small share and is lost in the noise at this load.
- `post-tool` pays the full import to answer `{}`; the kit's `sh` guard is what keeps it off the
  path (it runs only in relay mode). `session-end` with `clear` pays the full import to remove one
  file.
- Per session these add up to one import per user prompt (`prompt`), per turn end (`stop`, often
  twice: the second with `stop_hook_active` true still imports), per start, clear, resume and
  compact (`session-start`), and per end.
- The SessionEnd goodbye was not measured here: it needs a homeserver. Its offline part is the
  `session-end exit` row (load the bundles, find the seats); the posting adds per team the
  verify's 5 or more GETs, the outbox write and the PUT, in parallel threads within the 4 s
  budget. With Claude Code's default 1.5 s SessionEnd bound, an import of 0.2 to 0.4 s plus those
  requests is why the session settings raise the bound to 8 s (RESEARCH-pingbus 5.01).
- For the Go port, the comparison to make is the same world and payloads through `pingbus-hook`;
  DESIGN.md 8.2 expects a Go start of about 5 ms.
