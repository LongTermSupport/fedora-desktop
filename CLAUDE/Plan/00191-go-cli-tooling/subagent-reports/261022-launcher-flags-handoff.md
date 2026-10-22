# Handoff: Plan 00191 Task 4.5 phase 2, remaining standalone flags

Worktree branch `agent-a904808e2a2adf2f6-b394080d` (not pushed; do not push). It merged
`integration/261021-membership` (--no-ff), then commit `ed051791` (CCY 3.99.14.5).

## Done (ed051791)

- (a) Clock: `ccylaunch.Clock` (Now, ModTime(fs.FileInfo)), `SystemClock`, `Front.Clock`
  (nil = system) and `Front.Dir` (added, not used yet: the directory a run starts in, "" = cwd).
  Pinned clock = `src-go/internal/ccylaunch/difftest/pinclock` (non-test package, reads
  `$CCY_DIFFTEST_SANDBOX/fakes.json` epoch+shift; `Pinned()` is the stat-shift rule, now shared
  with `fake_test.go`). Used by the in-process front (`difftest/front_test.go runFront`) and by
  `cmd/claude-yolo/clock_difftest.go` (`//go:build difftest`); `clock.go` (`!difftest`) is
  SystemClock. `go list -deps ./cmd/claude-yolo` has no pinclock. Build for LAUNCHER runs:
  `go build -tags difftest -ldflags "-X main.Version=<V>" -o untracked/scratch/lf/claude-yolo ./cmd/claude-yolo` with `untracked/scratch/lf/claude-yolo.bash` + `lib/*.bash` beside it
  (copy the checkout's launcher again after every bump).
- (b) Colour: matched bash (front passes colour=true to tokens.List; print_error colours always
  too). No harness allowance needed. T6 terminal-only colour deferred until bash retires.
- Routing: `flagKind`/`flagOf`/`flagTools`/`answer()` in front.go; `plan()` returns the flag;
  non-version/help flags hand over when Version empty or != bash's CCY_VERSION (F6 refusal
  only for --version/--help). `answer` returns (code, why); why != "" => hand over, decided
  before any output.
- `bashread.go`: bashName (`${fn%.*.token}`), readsAlike (bash date regex vs tokens.ParseFileName),
  isTokenFile, tokenGlob (sorted bytes; why on non-regular entry/unlistable dir),
  collationIsBytes (LC_ALL>LC_COLLATE>LANG is C/POSIX/C.\*), orderAlike (non-C: only
  `^[a-z]+\.YYYY-MM-DD\.token$`), zoneReadsAlike (TZDIR/ZONEINFO set => no; TZ loadable),
  colourAlike (bash days = truncated seconds between local midnights; DST day differs).
- `listtokens.go`: F10 `--list-tokens` alone; `poolWhy`, `printPool` (reuse for export's
  "Available tokens" listing). TestFrontAnswersItself covers list-tokens-\* (frontAnswerLines).
- Results: TestLauncherParity 82/82 bash and LAUNCHER=built front; TestFrontParity 82/82;
  qa-changed rc 0. Docs: docs/ccy.md front section, changelog 3.99.14.5, F10 in package doc.

## Left (one commit each, CCY bump each: 3.99.14.6, .7, .8; changelog + docs/ccy.md line)

### --top: leave in bash (reason for the report/PLAN)

show_container_top (lib/docker-health.bash) is an interactive loop (`read -rp "Action: "`,
refresh/stop actions, pgrep, stats) and needs a terminal; it also runs the whole in-repo
prelude. Keep `{"--top"}` in TestEverythingElseIsHandedOver (already there).

### --token-usage (pre-repo, line 424 of the launcher; lib/token-management.bash 152-1042)

Port usage_report exactly. Key facts:

- Args after the flag: each must be `--json`, else stderr
  `ccy --token-usage: unknown option X (usage: ccy --token-usage [--json])`, exit 64 (first bad).
- Env with `${:-default}` semantics: CCY_USAGE_ENDPOINT (https://api.anthropic.com/v1/messages),
  CCY_USAGE_MODEL (claude-haiku-4-5-20251001), CCY_USAGE_TIMEOUT 20, CCY_USAGE_CONNECT_TIMEOUT 5,
  CCY_USAGE_TTL 900 (hand over unless ^[0-9]{1,18}$), CCY_TOKEN_USAGE (=="0" off),
  CCY_USAGE_SCALE (fraction iff value=="fraction" after default).
- error precedence: off => "usage reading is turned off (CCY_TOKEN_USAGE=0)"; no curl on PATH
  => "needs curl (run play-claude-yolo.yml)"; token dir not a dir => "no token directory at DIR
  (create a token with: ccy --create-token)"; no names => "no tokens in DIR (create one with:
  ccy --create-token)". Hand over: poolWhy reasons plus any undated file, any name outside
  printable ASCII, cache path exists and not a dir, a to-fetch token unreadable or holding NUL,
  a CCY_USAGE\_\* value containing a token (refuse rather than leak to argv is also fine).
- Names: first file per bashName in glob order; later files of a name are "shadows".
- fetch = first files that are valid (text date > today). If any: human prints stderr
  `Reading usage for N account(s): 1 small API call each, unless read in the last TTL seconds...`.
- prime cache: cacheDir = Dir(tokenDir)+"/usage-cache", MkdirAll + chmod 0700; now2=Now; skip a
  name whose `<name>.result` line 1 == filename and 0 \<= now2-ModTime < TTL. Workers (goroutines):
  token = file content with trailing \\n stripped; empty => no result. mktemp x2 (hdr, body) in
  $TMPDIR (else /tmp), names `tmp.` + 10 [A-Za-z0-9] (harness normalises
  `{sandbox}/tmp/tmp.XXXXXXXXXX`), O_EXCL 0600, removed after.
  curl argv exactly: `--config - --silent --request POST --header 'content-type: application/json' --header 'anthropic-version: 2023-06-01' --header 'anthropic-beta: oauth-2025-04-20' --data {"model":"MODEL","max_tokens":1,"messages":[{"role":"user","content":"."}]} --dump-header HDR --connect-timeout CT --max-time T --output BODY --write-out %{http_code} ENDPOINT`; stdin `header = "Authorization: Bearer TOKEN"\n` (execx.Cmd.Stdin; never Secrets,
  which would put it in curl's env and trip the difftest secret check). code = stdout with
  trailing \\n stripped; any error => "000". Extract from HDR: only \\n-terminated lines, NULs
  dropped, one trailing \\r stripped, pattern `[Aa]nthropic-[Rr]atelimit-unified-*`, name =
  before first ':', value after first ':' minus one leading space, lowercase name ->
  5h-utilization u5, 7d-utilization u7, 5h-reset r5, 7d-reset r7, representative-claim claim
  (claim with \\t => ""). No u5 and no u7 => no record. Record `u5\tr5\tu7\tr7\tclaim`.
  Result `<filename>\n<code>\n<record>\n`, 0600, temp in cacheDir then rename to
  `<name>.result` (golden: cache dir 0700, result 0600).
- Rows (per name; now = first Now, today = now date): expires = bash date. Not valid => reason
  `token expires today, so ccy will not launch with it; renew: ccy --update-token=NAME` or
  `token expired on DATE; renew: ccy --update-token=NAME`. Else read result lines; line1 != fn
  => "no figures were fetched for this token file"; else fetched = ModTime (Clock!), http = code
  if ^[1-9][0-9]{2}$; mtime < now && now-mtime >= TTL => "this run's fetch did not complete, and
  the cached figures are out of date"; record "" => status reason (000 "could not reach the
  API", 401|403 "this token was not authorised to read it", 429 "rate limited", 200 "the API
  reported no limits", else "HTTP X"); else split on \\t (SplitN 5; claim = rest with \\t
  removed), per bucket (5-hour u5, 7-day u7): "" => warn "L: the API did not report it"; not
  ^[0-9]+(.[0-9]+)?$ => "L: the API sent a value that is not a number (RAW)"; > 2 (fraction) /
  200 (percent) => "L: SCALE MISMATCH, the API sent RAW, impossible under CCY_USAGE_SCALE=S";
  pct = fraction ? sprintf %.4f of x\*100 then %.2f : %.2f, trailing zeros then '.' stripped. Any
  pct => status ok, else reason "the API sent no usable figures". r5/r7 kept only if ^[0-9]+$.
  Shadows add warn: valid => "shadows a later file of this name, which ccy never uses: remove
  the one you do not want"; else "SHADOWED RENEWAL: ccy launches with this unusable file, not
  the later one; remove FN". error "" and no ok => "usage could not be read for any token".
- JSON (see goldens token-usage-json/api-down/expired-only/disabled): header lines, items
  `\n    {...}` joined by ",", end `\n  ]\n}\n` or `]\n}\n` when none; times `date -u` ISO Z;
  pct and http raw or null; strings escaped \\ " \\n \\r \\t then control runes \\u%04x.
- Table: columns NAME EXPIRES 5-HOUR RESETS 7-DAY RESETS STATUS, widths by len, format
  `%-*s  %-*s  %*s  %-*s  %*s  %-*s  %s\n`; pct label `%3s%%` or `%4s` "\<1%" when %.0f is 0 and
  value has a nonzero digit; reset text `in N units (YYYY-MM-DD HH:MM ZONE)` local zone
  (human reset: \<=0 "resetting now"; \<5400 minutes (d+30)/60; \<172800 hours (d+1800)/3600;
  else days (d+43200)/86400; plural unless 1; "resets " prefix dropped); status "ok\[; binding:
  LABEL\]" (five_hour "5-hour limit", seven_day "weekly limit", seven_day_opus "weekly limit
  (Opus)", seven_day_sonnet "weekly limit (Sonnet)", else raw) or "unavailable: REASON", then
  "; " + warns; shadow rows `NAME DATE - - - - shadowed: ccy --token NAME launches with FN`.
  Then stderr `ccy --token-usage: ERROR` if error. Exit 0 iff any ok, else 1.
- flagTools: date stat mktemp cat mv rm chmod mkdir dirname awk basename.
- Break line for TestFrontAnswersItself: `usage_report "$TOKEN_DIR" "$@" || ccy_token_usage_rc=$?`.
  Scenarios: token-usage-no-tokens/expired-only/disabled/bad-option/table/json/api-down
  (token-usage-not-first stays bash: `--no-ssh --token-usage`).

### In-repo prelude, then --prevent (exact args) and --export-token NAME (exact two args)

Bash lines 432-1101 of the launcher with these args are only: check_git_repo (`[ -d .git ]`),
check_ccy_gitignore_safety, ccy_env_local_dist_sync, check_allowed_hostname, restart marker,
migration, get_project_name (no output), mkdir -p + chmod 700 of $HOME/.claude-tokens/ccy,
.../tokens, .../projects. Hand over (pure checks, before writing anything) when:
CCY_RESTART_RELAUNCH, CCY_MAX_AGE or CCY_RELAUNCH_DEADLINE_EPOCH non-empty; no `.git` dir;
`.claude` or `.claude/ccy` exists and is not a dir; `.claude/ccy/.gitignore` or
`ccy.env.local.dist` exists and is not a readable regular file without NUL (or a version number
over 18 digits); `ccy.env.local` regular but unreadable; `.claude/ccy/sessions` or
`.claude/ccy/allowed-hostnames` exists at all (Lstat); `.claude/ccy/Dockerfile` is a regular
file; HOME empty/relative/unclean or a HOME dir exists as a non-dir; `git ls-files -z -- .claude/ccy` fails or lists anything (add a gitx method, e.g. `TrackedUnder`, with a test:
git is owned by internal/gitx per project_seam_commands). Extra flagTools: git cat grep mkdir
chmod basename tr sed dirname awk (+ date ls for export).
Writes (bash order): mkdir .claude/ccy (0777&umask); gitignore: content minus trailing \\n has
no line "\*" => stdout "✓ Creating .claude/ccy/.gitignore for security" (absent) or
"\\033\[0;33m⚠ Updating .claude/ccy/.gitignore (was missing protection rules)\\033\[0m", write
expected+"\\n" (text: common.bash check_ccy_gitignore_safety `expected_gitignore`); then if no
line exactly "!mounts" stdout "✓ Adding the mounts exception to .claude/ccy/.gitignore",
append "!mounts\\n" (raw append, even onto a last line without newline); same for
"!ccy.env.local.dist" ("✓ Adding the ccy.env.local.dist exception to .claude/ccy/.gitignore").
Dist sync (common.bash 669-765, version 3, text = heredoc ccy_env_local_dist_text): absent =>
stderr "✓ Writing .claude/ccy/ccy.env.local.dist (version 3); commit it" + write text+"\\n";
first line `^# ccy\.env\.local\.dist version [0-9]+$` with N > 3 => stderr yellow "⚠
.claude/ccy/ccy.env.local.dist (version N) was written by a newer ccy than this one (version
3); left as it is. Update ccy here." and current=N; else content minus trailing \\n != text =>
stderr yellow "⚠ Updating .claude/ccy/ccy.env.local.dist to version 3; commit it" + write.
Local file regular: first line `^# based on ccy\.env\.local\.dist version [0-9]+$` absent =>
yellow "⚠ .claude/ccy/ccy.env.local has no '# based on ccy.env.local.dist version N' line." +
" Compare it with ccy.env.local.dist and add the line where it is placed (its IaC)."; B \<
current => yellow "⚠ .claude/ccy/ccy.env.local is based on ccy.env.local.dist version B; the
dist is now version C." + " Review: diff .claude/ccy/ccy.env.local.dist .claude/ccy/ccy.env.local"

- " Update it where it is placed (its IaC), not in the checkout." (yellow = \\033\[0;33m ...
  \\033\[0m, whole first line). Hold both embedded texts to common.bash by tests (as help.txt).
  Then the HOME mkdirs. --prevent: append "never\\n" to .claude/ccy/allowed-hostnames, stdout
  "✓ CCY disabled for this project.\\n Written 'never' to .claude/ccy/allowed-hostnames\\n\\nTo
  re-enable CCY, remove or edit that file:\\n rm .claude/ccy/allowed-hostnames\\n", exit 0. Break
  line: `echo "never" >> "$allowed_file"`.
  --export-token NAME (lib/token-management.bash 1814-1937): NAME must match
  ^[A-Za-z0-9\_-]+$ and not be "--" (else hand over; "" or "--" is the interactive export, which
  stays bash: it is a read loop). Matches = glob `NAME.*.token` (prefix "NAME.", suffix ".token",
  len >= len(NAME)+7), byte order with the orderAlike rule, all regular. None => stderr
  print_error "No token found with name: NAME" (`\033[0;31mERROR:\033[0m MSG`), stdout
  "\\nAvailable tokens:\\n" + printPool (with poolWhy checked first), exit 1. Else LAST match;
  bash date regex none => print_error "Token file has invalid format: FN", 1; date \<= today
  (text) => print_error "Token 'NAME' is expired (DATE)" + stdout "Create a new token first: ccy
  --create-token", 1; content minus trailing \\n empty => print_error "Token file is empty:
  DIR/FN"; contains "'" => print_error "Token contains unexpected characters"; else stdout
  "# CCY Token: NAME | Expires: DATE | Generated: \<local %Y-%m-%d %H:%M:%S>\\n# Paste into terminal
  on target machine, or save in LastPass/1Password.\\n# Other files of this name: asks before
  removing them; CCY_TOKEN_IMPORT_REMOVE_OTHERS=1 removes, =0 keeps.\\nbash \<<'CCYTOKEN'\\nset -eo
  pipefail\\nname='NAME'\\ndate='DATE'\\ntoken='CONTENT'\\n" + BODY (the quoted heredoc, held to the
  lib by a test), exit 0. Hand over if the chosen file is unreadable or holds NUL. Break line:
  `export_token "$TOKEN_DIR" "$EXPORT_TOKEN_NAME"`. Scenarios: prevent, export-token-named,
  export-token-missing (export-token-interactive and top-none stay bash).

### Then

- PLAN.md Task 4.5: add the phase 2 cut-over bullet (flags, how (a) and (b), handover rules,
  difftest results, --top and interactive export left in bash with reasons). Phase 2 done;
  phase 3 per D4/D10 waits for the owner (the launch path itself: argument parsing and the
  in-repo prelude for a real launch). Keep the ui.Lines follow-ups open (nothing prompts).
- qa-changed before each commit; never qa-all; no push.
