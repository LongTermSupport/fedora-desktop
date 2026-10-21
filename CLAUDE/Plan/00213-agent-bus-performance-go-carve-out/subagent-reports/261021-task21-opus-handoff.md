# Task 2.1 handoff: Opus review of the Haiku draft, first half

Branch `agent-ac060bdc268f15ede-71631d51`, on top of the draft (head 6e6bd0c5). Stopped at the
context budget. The WIP commit holds the two new pieces below; the statusline package itself is
still the draft. Golden: still 101/104 (nothing in statusline changed yet).

## Done (committed, lint clean, tests green with -race)

- **`internal/execx/session.go`** replaces the draft's `NewSession` (which was wrong, see D1).
  `Cmd.NewSession` now routes `OS.Run` to `runSession`: setsid; the run lasts until the child
  exited AND stdout reached EOF (Python's `communicate`); when ctx ends, `kill(-pgid, SIGKILL)`
  whether or not the shell already exited, then at most `Grace` for the pipes, then ctx's cause.
  Stdin is fed through an owned pipe (EPIPE ignored); a `*os.File` stream is handed over as is.
  Tests in `session_test.go` (grandchild holding stdout waited for; deadline kills the group after
  the shell exited; an escaped `setsid` holder does not block past Grace; 1 MiB unread stdin;
  exit code and signal; start failure; done ctx starts nothing).
- **`internal/pingbus/pyjson/`**: CPython-compatible JSON (S23). `Decode` (a str), `DecodeUTF8`
  (`raw.decode("utf-8")` then loads), `DecodeBytes` (`json.loads(bytes)`: detect_encoding UTF-8/16/32
  plus surrogatepass), `Options{NoDuplicateKeys}` for member.json, `Number{Lit,Kind}` (1 vs 1.0),
  `Null{}`, `Object`, `EqualsInt` (Python `v == 1`), `Number.IsInt` (`type is int and ==`),
  `Canonical` (the `json.dumps(sort_keys=True)` identity `_list_of` dedups by). Every case in the
  tests was checked against the container's Python 3.11.2. 97% coverage.

## Draft defects found (fix in the statusline package)

- **D1 (fixed in execx)**: with exec's WaitDelay, a grandchild in the group holding stdout after
  the shell exited was never killed (exec calls Cancel only before exit) and the run failed with
  ErrWaitDelay after 3 s; `runInner` then returned a non-cancel error and `Run` printed an EMPTY
  line, dropping the segment. Python waits to the 10 s timeout, kills the group, shows the note.
- **D2**: `render` returning any non-cancel error blanks the whole output (`Run` prints `""`).
  Only a cancel may end the render; an inner start failure must become a red note.
- **D3**: `main` blocks in `io.ReadAll(stdin)` with no cancel: a SIGTERM while Claude Code holds
  stdin open never exits. Read stdin in a goroutine and select on the cancel. A stdin read error
  or a Getwd error must not exit 1 (S3): use an empty payload / the process's own cwd.
- **D4**: `envMap` keeps the LAST duplicate; CPython's os.environ and Go's getenv keep the FIRST.
- **D5**: the inner env pairs come from ranging a map (gqaci_map_range_order); sort them.
- **D6**: `/bin/sh -c` trips `project_seam_commands` (shell -c). It is the spec (Claude Code runs
  a statusLine command through a shell). Needs an allowance table for shell strings in
  `src-go/qa/rules` (like `dynamicNames`, with its fixture red/green) and a GoProjectRules.md row.
- **D7 (roles.go)**: member.json is opened WITHOUT O_NOFOLLOW in Python (`_read_member_file`);
  the draft refuses a symlinked member.json. The linked bundle check reads ONE link level
  (`link.parent / os.readlink(link)`, then lstat; a link onto a link is refused); the draft's
  EvalSymlinks accepts a chain. The token mode test is `S_IMODE & ~0o600` (setuid/sticky bits
  count); the draft's `Perm()&0o177` misses them.
- **D8**: `decodeJSON` (encoding/json) is replaced by pyjson everywhere: payload, settings and
  membership through `DecodeUTF8`, member.json through `DecodeUTF8` with NoDuplicateKeys,
  team.json through `DecodeBytes`. `"{} }"` was accepted by the draft's `dec.More()` check.
- **D9 (width.go)**: Python's zero width is `unicodedata.combining(ch) != 0` (canonical combining
  class) or category Cf, not Mn/Me. No x/text in vendor: generate the W/F and zero-width range
  tables from the container's Python (Unicode 14) into a Go file, with a test pinning samples.

## Left to do

1. Port `_parse_member` fully (config.py): unknown keys, required keys, `protocol` IsInt(1),
   `team`, `server_name` DNS (`[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?` labels, \<= 253),
   `user_id` = `@<handle>:<server>` with `protocol.parse_handle` (HANDLE_PATTERN, type group;
   reserved/human localparts do not count for a member), `admin` = `@admin:<server>`, room
   `![A-Za-z0-9_-]{43}`, `token_file`, `plain_http_hosts` (list of str, no `%`, canonical form
   equal to the entry, distinct; Python 3.11 prints an IPv4-mapped IPv6 as hex `::ffff:102:304`,
   so format IPv6 by hand from As16 with the RFC 5952 longest-first-zero-run rule), `base_url`
   (`check_base_url`: printable ASCII; no `?`/`#`; urlsplit scheme = chars before the first `:`
   if first is alpha and all in letters/digits/`+-.`, lowercased, must be http/https; rest must
   start `//`; netloc to the first `/`; path must be empty; `@` refused; brackets: unmatched or
   data around them refused, inside must be `v[hex]+\..+` or an IPv6 address (IPv4 refused);
   port digits only and \<= 65535, empty port is fine; hostname lowercased; http only to a
   listed IP; https host IP or DNS name), `human_text` bool, `limits` (object, keys in BOUNDS,
   int not bool within bounds; explicit null refused). Python CRASHES (uncaught ValueError) on
   the bracket refusals; Go refuses instead (an S3 departure to record).
2. `host` member: needs the link AND `element_accepted(passwd home)`, else `_refuse_beside_element`
   (`.var/app/im.riot.Riot` lexists, or `.config` is_dir with an entry `Element` / `Element-*`).
   Inject the passwd-home lookup; unit tests through a fake (no oracle fixtures).
3. Port `protocol.parse_team_record` and `inbox.load_cached_record` (team.json O_NOFOLLOW,
   `_list_of` with Canonical dedup, humans/roles/path_prefixes/forge_api (Python `\s` = Go
   unicode.IsSpace plus U+001C..U+001F)/org/repos/exclude, "org or a repository", then this
   member's role). This is the 3 failing golden steps.
4. The cache (S32 to S36): `<home>/statusline-cache.json` 0600 via `fsx.WriteAtomic`; key = session
   id, uid and the listed env vars; entries hold segment text, the role stderr line, the lstat
   fingerprints (plus stat when the path is a link) of every path the render touched, taken
   BEFORE reading each (so a change between fingerprint and read only costs a miss), and expiry
   = min(written + 60 s, each shown fresh down mark's mtime + 300 s). Clock as `Input.Now`; the
   golden's `goRender` can take `clockAt(t)`. A cache read/write failure never changes output.
5. Lint (13 draft findings above) and coverage, then golden 104/104 in-process and with
   `-impl=<built binary>`, then the journal entry via `mkplan.bash --journal`.
