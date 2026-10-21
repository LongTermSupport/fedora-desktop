# SPEC: `pingbus status-line` (Plan 00213 Tasks 2.1 and 2.2)

The behaviour the Go status line must have. The current implementation is the Python in
`helpers/pingbus/statusline.py`. When the bus segment is shown it also runs the role read in
`config.py`, `inbox.py`, `protocol.py`, `limits.py` and `membership.py`. Each expectation below
has a number (S1 to S40), so a Go test can cite it. The parity oracle (section 10) pins sample
cases for each one.

The parts marked **NOT BOILERPLATE** are the ones a port is likely to get wrong. Each of them
carries a Python behaviour that Go's standard library does not give by default.

## 1. Invocation and exit

- **S1** Claude Code runs `pingbus status-line` once per render, with the session JSON on
  stdin. It reads at most 1 MiB + 1 byte of stdin (`INPUT_MAX_BYTES + 1`). The environment is
  Claude Code's, including `COLUMNS`. No arguments are read.
- **S2** stdout is the status line followed by exactly one `\n`. When the line is empty, stdout
  is just `\n`.
- **S3** **NOT BOILERPLATE: it always exits 0.** Claude Code blanks the whole line on a
  non-zero exit. A failing, hung or unreadable inner line, an unreadable setting, record,
  membership or role: each is shown in red, never turned into an exit status. The only
  non-zero exit is a cancel (S12).
- **S4** stderr carries the inner command's own stderr, which it inherits, and at most one
  role diagnostic line (S24). Nothing else.

## 2. The inner status line

- **S5** If `PINGBUS_STATUS_LINE_INNER` is set to any non-empty value, no inner line is run
  (this is the loop guard). Otherwise the inner command is looked for.

- **S6** The project directory is the first absolute path among, in order: the payload's
  `workspace.project_dir`, `$CLAUDE_PROJECT_DIR`, and the payload's `cwd`. A relative or
  non-string value is skipped.

- **S7** The settings files are read in order, and the first usable `statusLine.command` wins:

  1. `<project>/.claude/settings.local.json`
  2. `<project>/.claude/settings.json`
  3. `$CLAUDE_CONFIG_DIR/settings.json`, or `$HOME/.claude/settings.json` if `HOME` is
     absolute (with no `CLAUDE_CONFIG_DIR` and a relative `HOME`, there is no user file)

  A missing file is passed over. So is a file whose top level is not an object, or whose
  `statusLine.command` is not a string, is blank after trimming, or is exactly `pingbus status-line` after trimming. The command used is the untrimmed string.

- **S8** **NOT BOILERPLATE: the wording of the settings errors.** A file that exists but cannot
  be read or decoded stops the search. The inner part then becomes
  `ESC[31mstatus line: <path> cannot be read (<PythonExceptionName>)ESC[0m`, or
  `... <path> is not valid JSON ...`. The exception name is part of the output:
  `IsADirectoryError` and `UnicodeDecodeError` appear in the goldens. Go must map its errors to
  the same names: errno EISDIR becomes IsADirectoryError, EACCES becomes PermissionError, and
  invalid UTF-8 becomes UnicodeDecodeError. PermissionError cannot be recorded, because the
  oracle runs as root in the container, so a Go unit test must cover it. The alternative is a
  recorded departure.

- **S9** The command runs as `/bin/sh -c <command>`. Its stdin is the raw payload bytes. Its
  environment is this process's, plus `PINGBUS_STATUS_LINE_INNER=1`, and nothing else is
  added. Its working directory is the project directory if that is an existing directory,
  else this process's own. Its stdout is captured. Its stderr is inherited.

- **S10** **NOT BOILERPLATE: its own process group.** The inner starts in a new session
  (`setsid`, Python's `start_new_session=True`), so that its pid is its process group id.
  Everything it starts can then be killed with one `kill(-pgid, SIGKILL)`.

- **S11** **NOT BOILERPLATE: the timeout.** At 10 s (`INNER_TIMEOUT_S`), the whole group gets
  SIGKILL. The output is then *only* the note `ESC[31m(status line timed out after 10 s)ESC[0m`; any text printed before the timeout is dropped. The number is formatted as
  Python's `{:g}`. Nothing the inner started may outlive the render: a grandchild that holds
  the stdout pipe must not keep the render waiting past the kill. After the kill, Python
  drains the pipe with `communicate()`. Go must not block on a pipe that a killed grandchild
  in another group still holds. Because the kill reaches the whole group, the grandchild is in
  it too.

- **S12** **NOT BOILERPLATE: cancel.** Claude Code cancels a render still in flight when a new
  trigger arrives (code.claude.com/docs/en/statusline). On SIGTERM, SIGINT or SIGHUP the inner
  group is killed and the process exits `128 + signum` with nothing on stdout. A signal that
  arrives while the inner is being started, before its pid is known, is held. The group is
  killed as soon as the pid exists, and only then does the process exit. A signal with no
  inner running ends the process at once. This is the only non-zero exit.

- **S13** When the inner exits 0, its stdout is decoded as UTF-8, with each invalid sequence
  becoming U+FFFD (Python's `errors="replace"`; Go's `strings.ToValidUTF8` is not the same
  per-byte replacement, so test it). Trailing `\n` characters are then stripped, and nothing
  else.

- **S14** **NOT BOILERPLATE: a non-zero status.** The output is `<text> ESC[31m(status line exit N)ESC[0m`, or the note alone when the text is empty. For an inner killed by a signal, N is
  **negative** (`-15`): that is Python's `returncode`, and Go must reproduce it from
  `WaitStatus.Signal()`.

- **S15** The inner line is never cached. It runs on every render (the inner hooks-daemon line
  writes ccy `--supervise`'s context sidecar, Plan 00213 Task 1.3).

## 3. Which teams the segment shows

- **S16** If `PINGBUS_MEMBERSHIP` is set, is not `off`, and starts with `/`, the teams come
  from the membership (S17). Otherwise they come from the session record (S18). A relative
  value or `off` means the record.
- **S17** The membership, Plan 00203 H14. The teams are the seats held in the applied file
  that the desired file names, in desired order.
  - The desired file is `$PINGBUS_MEMBERSHIP/membership.json`.
  - The applied file is `$PINGBUS_HOME/applied.json`. `PINGBUS_HOME` must be absolute, else
    the membership is unreadable.
  - Each file is read without following a symlink and must be a regular file of at most
    65536 bytes. It must be a JSON object with `"v": 1` and `seats`, a list of objects whose
    `team` (a team name) and `seat` are strings. For the applied file, only seats with
    `"state": "held"` count.
  - A missing applied file means no teams yet, and no segment.
  - Any other failure gives the red note `this session's membership is unreadable (pingbus status)`.
  - An empty intersection gives no segment.
- **S18** The session record is `$PINGBUS_SESSIONS_DIR/<session_id>`, or
  `/tmp/pingbus-sessions-<uid>/<session_id>` when that variable is unset or empty.
  - `session_id` is the payload's, and must match `[A-Za-z0-9_-]{1,128}`.
  - The record must be a regular file (lstat, never followed) owned by this uid, with no group
    or other bits (`mode & 0o077 == 0`), and at most 4096 bytes.
  - Its content is ASCII team names separated by `,`, distinct, each matching
    `[a-z][a-z0-9-]{0,23}`, with no newline.
  - A payload that is not a UTF-8 JSON object of at most 1 MiB reads as `{}`.
- **S19** With no usable record: if `PINGBUS_TEAMS` is non-empty, the segment is the red note
  `no record of this session's teams (pingbus status)`; otherwise there is no segment.

## 4. Roles (`read_roles`)

This section is **NOT BOILERPLATE**. It is the largest part of the port, because Go must read
every on-disk format exactly as Python accepts it. A file Python refuses must give `(role?)`
in Go, and a file Python accepts must give its role.

- **S20** The home is `config.resolve_home`:

  - `$PINGBUS_HOME` if it is set. It must be absolute; a relative value is a refusal.
  - Else `$XDG_CONFIG_HOME/pingbus` if `XDG_CONFIG_HOME` is absolute (a relative one is
    ignored).
  - Else `$HOME/.config/pingbus` if `HOME` is absolute.
  - Else a refusal.

  If the home cannot be resolved, every team is `(role?)`, with the diagnostic `pingbus status-line: no team's role can be read: <reason>`.

- **S21** For each team, `config.load_bundle(home, team)` runs. Every refusal is a role that
  cannot be read:

  - The team name is checked.
  - If `home/<team>` is a symlink, its target must be a real directory (not another link)
    owned by this uid with mode exactly 0700.
  - `member.json` is opened non-blocking (a FIFO cannot hang the read), must be a regular
    file of at most 65536 bytes, and must be strict UTF-8 JSON. **A duplicate key anywhere is
    a refusal.** The top level must be an object.
  - `_parse_member` then applies:
    - The required keys are present, and no keys other than the required and optional ones.
    - `protocol` is the integer 1 (`type is int`: `1.0` and `true` are refused).
    - `team` equals the directory name.
    - `server_name` is a lowercase DNS name of at most 253 characters.
    - `user_id` is `@<handle>:<server_name>` with a valid handle (`protocol.parse_handle`,
      whose type is one of podman, lxc, docker, vm, host).
    - `admin` is `@admin:<server_name>`.
    - `room` is a room ID.
    - `token_file` is `"token"`.
    - `plain_http_hosts` is a list of distinct IP literals in canonical form (Python's
      `ipaddress` canonical form, which Go's `netip` must be checked against for IPv6), with
      no `%`.
    - `base_url` satisfies `check_base_url` (`urllib.parse.urlsplit` rules: no query,
      fragment, credentials or path; a valid port; `http` only to a listed IP).
    - `human_text`, if present, is a bool.
    - `limits`, if present, passes `limits.parse_limits`.
  - The token file is opened with O_NOFOLLOW|O_NONBLOCK. It must be a regular file owned by
    this uid, mode 0600 or stricter, at most 4096 bytes, and printable ASCII `[\x21-\x7e]+`,
    so a trailing newline is refused.
  - A `host` member also needs its bundle reached through a link and the Element acceptance
    record (`config.element_accepted`); otherwise no Element profile may exist under the
    passwd home (`_refuse_beside_element`). The oracle has no `host` fixtures, because they
    read the real passwd home; Go unit tests must cover this through a fake.

- **S22** Then `inbox.load_cached_record`:

  - `<bundle>/state/team.json` is opened with O_NOFOLLOW|O_NONBLOCK, must be a regular file
    of at most 65536 bytes, and is parsed as JSON. **Python's `json.loads(bytes)` detects
    UTF-8, UTF-16 and UTF-32 from the byte pattern**, unlike `member.json`, which is decoded
    as UTF-8 first.
  - `protocol.parse_team_record` then applies:
    - The keys are exactly the required ones plus any of `org`, `exclude` and
      `path_prefixes`.
    - `v` is the integer 1.
    - `team` matches the team.
    - `humans` holds 1 to 16 human user IDs of this server.
    - `roles` is an object of at most 64 entries mapping agent user IDs to `coordinator` or
      `worker`, with the legacy `orchestrator` read as `coordinator`.
    - `path_prefixes` holds 1 to 32 prefixes.
    - `forge_api` matches its pattern.
    - `org` is an owner.
    - `repos` and `exclude` follow `_parse_repos` and `_parse_exclude`.
    - There is an org or at least one repository.
  - This member's `user_id` must have a role in `roles`.

- **S23** **NOT BOILERPLATE: JSON is Python's JSON.** Go's `encoding/json` differs from it:

  - It refuses `NaN` and `Infinity`, which Python accepts.
  - It cannot tell `1` from `1.0` without `json.Number` and a check of the literal.
  - It accepts invalid UTF-8 inside strings, which Python's strict decode refuses for
    `member.json`.
  - Python accepts lone surrogate escapes (`"\ud800"`).

  For a duplicate key in `team.json` and in the settings files, the last value wins in both.
  `member.json` refuses duplicates (S21). The port needs a decoder that agrees with Python on
  every one of these cases, with a unit test for each.

- **S24** One stderr line, when any role failed: `pingbus status-line: the role cannot be read (<team>: <message>; <team>: <message>)`. The messages are the Python `ConfigError` and
  `Untrusted` texts, which include paths and, in some cases, Python exception names
  (`member.json is not valid JSON (ValueError)` for a duplicate key, `(JSONDecodeError)` for
  bad syntax).

  **Decided** by the coordinator, on the reviewer's advice: the Go wording may differ, and it
  carries no Python exception names (S37). Stderr is compared by shape, not byte for byte:

  - exactly one line, and only when a role failed
  - the prefix `pingbus status-line: the role cannot be read (`, or the S20 line
  - the failed teams in order, separated by `; `
  - an empty stderr stays empty, and the inner's stderr (S4) still passes through byte for
    byte

  stdout and the exit code stay byte-exact. The Go golden test pins this as the numbered
  departure S37 in `deviations.json`.

## 5. Down marks (Plan 00203 H26)

- **S25** A team shows ` (down)` after its role when `$PINGBUS_HOME/down/<team>` is a regular
  file (lstat, never followed) whose mtime is less than 300 s old (`DOWN_FRESH_S`).
  `PINGBUS_HOME` must be absolute for this; `resolve_home`'s fallbacks are not used here.

## 6. The segment's text and wrapping

- **S26** Each team is shown as `<team> (<role or role?>)` followed by each of its marks. All
  but the last are followed by `,`. The pieces are joined with a space after `👥 `.
- **S27** **NOT BOILERPLATE: the width measure** (`display_width`). Colour escapes take no
  columns. Code points of East Asian width W or F take 2 columns. Combining marks and format
  characters (category Cf) take 0. Everything else takes 1. The segment's own text is ASCII
  plus `👥` (2 columns), so a Go table needs only to agree with Python's `unicodedata` on the
  label, but the function should match `unicodedata` for any input it may later be given.
- **S28** `COLUMNS` is used only if it is 1 to 6 decimal digits with a value above 0, so `040`
  is 40, and `+40`, ` 40`, `0` and `1234567` are unusable. With no usable width, the segment is
  one row.
- **S29** The rows are built greedily: a piece joins the current row if the row's width would
  stay at most `COLUMNS`. Otherwise it starts a new row, indented by `display_width("👥 ")`,
  which is 3 spaces. A piece wider than the terminal keeps a row of its own, uncut. Each row
  is wrapped in its own colour span, `ESC[36m…ESC[0m` (or `ESC[31m` for a note), and the rows
  are joined with `\n`.
- **S30** The red notes (S17, S19) are split on single spaces and wrapped the same way.
- **S31** The whole output is the inner text, then `\n`, then the segment. If either one is
  empty, only the other is shown, and if both are empty the output is empty. The final `\n`
  of S2 always follows.

## 7. The bus segment's cache (Go only; the owner's requirement)

The hooks daemon's refresh and the bus refresh have separate TTLs. The inner line runs on
every render. The segment is cached.

- **S32** The cache is one file under the resolved home (S20):
  `<home>/statusline-cache.json`, mode 0600, written atomically (a temporary file, then
  rename). It is keyed by everything the segment depends on that is not on disk: the session
  id, the uid, and the environment variables `PINGBUS_MEMBERSHIP`, `PINGBUS_TEAMS`,
  `PINGBUS_HOME`, `XDG_CONFIG_HOME`, `HOME`, `PINGBUS_SESSIONS_DIR` and `COLUMNS`. If the home
  cannot be resolved, nothing is cached.

- **S33** **NOT BOILERPLATE: invalidation.** The entry records an lstat fingerprint of every
  path the uncached render read or looked for: dev, ino, size, mtime in ns, ctime in ns, mode
  and uid, or "absent". These paths are:

  - the membership desired and applied files
  - the session record
  - each `home/<team>` entry, and its link target when it is a link
  - each `member.json`, `token` and `state/team.json`
  - each `down/<team>`
  - for a `host` member, the Element paths

  The entry is used only if every fingerprint is unchanged. ctime is what catches a `chmod`
  of the token. ino catches an atomic replace. ns mtime and size catch an in-place rewrite of
  the same size (the oracle's `cache-role-same-size-change`).

- **S34** The entry expires at the earliest of: 60 s after it was written (the owner's TTL),
  and, for each fresh down mark it shows, that mark's mtime + 300 s (the moment the mark stops
  counting with no file changing). A Go unit test with an injected clock proves this. The
  oracle uses no wall clock: its `cache-down-ages-out` re-dates the mark, which goes through
  S33.

- **S35** A cached render gives byte-identical stdout and stderr, so the role diagnostic of
  S24 is stored and printed again. This is what the oracle's fresh-cache and invalidated-cache
  steps check.

- **S36** A cache that cannot be read or written never changes the output and never changes
  the exit status (S3). The render proceeds uncached.

## 8. What the port reads, owned elsewhere

These formats are owned by other modules, and the Go port must stay in step with them.
`TestReadsWhatTheRealWritersWrite` (`internal/pingbus/statusline/writers_test.go`) has each one
written by its real writer on every run (the Python ones through `testdata/write_formats.py`,
`membership.json` by the Go `membership.Write`) and requires the render to read them all, so a
writer's change of format fails a test. The bundle is the admin's (`agent_bus.admin`'s bundle,
unpacked by `agent_bus.checkout.read_bundle`):

| File                                        | Owner (writer)                           | Reader in Python                                  |
| ------------------------------------------- | ---------------------------------------- | ------------------------------------------------- |
| `$PINGBUS_SESSIONS_DIR/<id>`                | `hooks.record_session_teams`             | `statusline.session_teams`                        |
| `$PINGBUS_MEMBERSHIP/membership.json`       | ccy-teams / `membership`                 | `statusline.membership_teams` (a copy, test-kept) |
| `$PINGBUS_HOME/applied.json`                | the seat keeper (`membership`)           | same                                              |
| `$PINGBUS_HOME/<team>/member.json`, `token` | `agent-bus-install` / ccy-teams (config) | `config.load_bundle`                              |
| `$PINGBUS_HOME/<team>/state/team.json`      | the watcher (`inbox.TeamState`)          | `inbox.load_cached_record`                        |
| `$PINGBUS_HOME/down/<team>`                 | the watcher (`statusline.set_team_down`) | `statusline._is_down`                             |

A change to a status-line input or output in the Python is followed by a re-record of the
oracle (`go test ./internal/pingbus/statusline/golden -update`) in the same change, and the
golden diff is reviewed.

## 9. Candidate departures (to decide, not yet taken)

- **S37** The Python exception names in S8 and S24 are an implementation detail leaking into
  user-visible text. For S24 this is taken: the Go wording may differ, compared by shape. For
  S8, which is on stdout, the names are still to be kept, or changed in both implementations
  together.
- **S38** S14's negative exit for a signal. `(status line killed by SIGTERM)` would be clearer.
  Changing it means changing the Python in the same commit, or recording a departure.
- **S39** S11 drops any text the inner printed before it timed out.

Departures the Go port has taken (Task 2.1), each where the Python crashes or has no answer:

- **D2** An inner command that cannot be started (its working directory gone, `/bin/sh`
  missing) shows the red note `(status line could not be run)`. Python's `Popen` raises
  there. Only a cancel ends the render.
- **D3** A stdin that cannot be read is no payload, and a working directory that cannot be had
  is an empty one; neither exits non-zero. A signal during the stdin read still ends the
  render with 128 plus the signal.
- **D4** A name repeated in the environment keeps its first value, as Python's `os.environ`
  does; the inner command's environment is passed sorted by name.
- **Refusals** A `base_url` whose brackets `urlsplit` raises on is refused (Python crashes); a
  host member with no passwd home is refused (Python raises `KeyError`).
- **The racy rule (S33)** A render that read any path whose ctime is within 2 s of its clock is
  not cached: a same-size rewrite inside one file-system tick keeps every other mark of the
  fingerprint. The oracle's worlds are built moments before each step, so its in-process Go
  run sets the clock 3 s past the world's newest change: every scenario then meets the real
  cache, and each step of a `cache-` scenario states whether the cache file must be newly
  stored or left unchanged (a hit). The unit tests prove the hits by poisoning the stored
  segment.

## 10. The parity oracle

- **S40** The oracle lives where `src-go/CLAUDE.md` puts golden replays:
  `src-go/internal/pingbus/statusline/golden/testdata/`. It holds `scenarios.json` (literal
  worlds: files with content, mode and age, the environment, stdin, steps) and
  `golden/<scenario>/<step>.{stdout,stderr,exit}`, recorded from the Python zipapp built from
  this checkout and run through its own shebang. `{world}` stands for the temporary world's
  root. The steps of a scenario share one world, so a cache meets fresh and invalidated
  states, while the Python renders each step from scratch.
- The recorder and checker is the Go test `internal/pingbus/statusline/golden`
  (`golden_test.go`); the Python builders were retired with `capture.py`, and
  `scenarios.json` is now the hand-edited source:
  - `go test ./internal/pingbus/statusline/golden -update` re-records `golden/` from the
    Python zipapp built from this checkout.
  - The default run judges the Go `statusline.Run` in-process; `-impl=python` judges the
    zipapp; `-impl=<command>` judges any binary (split on spaces, run without a shell).
  - Steps can cancel with a signal once the inner has started (it creates `inner-started`),
    and can require pids from pid files to be gone afterwards (S10 to S12).
- stdout and the exit status are byte for byte. stderr is byte for byte except the one role
  line, which is compared by shape (S24, S37): the same prefix and the same failed teams. No
  `deviations.json` is kept, because the shape rule is the pinned departure itself.
