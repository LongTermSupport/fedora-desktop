# M3 round 2: the remaining owned-value rows, the UNSURE rulings, and tests/ and scripts/

Scope: rows B3, B6, B13, A15, the 6 UNSURE rows and the (b) rows of
`261022-m3-owned-value-independent-search.md`, plus a new search of tests/ and scripts/
(tests/helpers/plan_acceptance/ not opened). I changed no product file (nothing under helpers/, files/,
src-go/, extensions/, playbooks/, run.bash) and committed nothing. The two untracked 00211
`subagent-reports/261022-a3-m3-*round2.md` files in `git status` are not mine.

## Task 1: the four named rows

| Row | Fix                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B3  | `00201-*/m7_check.py` `board_table()`. It reads `var headings = [columns]string{...}` from `src-go/internal/teams/board/table.go` (exactly one, at least four names; positions 0-3 are colTeam..colState). It also reads `KindTeam Kind = "..."` (exactly one), because the team row's KIND cell `"team"` was a second copy of a named Go const. `expect_list_row` takes an optional `table` and builds its heading search and its row from what it read.                                                                                                                                                                                                                               |
| B6  | `00203-*/_acceptance-m3.inc.bash` `m3_removal_words` (in `m3_check_remove`). It makes one `python3 -I` call into this checkout and prints `cli.FAREWELL_TEXT` after `{handle}` (the text must be `{handle}` then fixed text, or it fails) and `membership.FAREWELL_REASONS["removed"]`. The search's row named only the `say` refusal at :269. Line :263 was a second copy: the room's goodbye `"left the team: removed by the owner"` is `cli.FAREWELL_TEXT` (named, cli.py:2835), which `pingbus seat farewell` posts. remove.go:125's string is only its step note. Both now use what was read.                                                                                      |
| B13 | `00214-*/check-changed-plays.bash` `login_words`/`read_login_words`. It reads `login_message._RUNS_CHANGED` and gets the command from `login_message.run_changed_command({"plays":[{"state": freshness.STALE}]}, checkout=root)`. That replaces `REMEDY`, the retyped `", or ... to run every play"`, and `expected_command`, which was a hand re-implementation of `shlex.quote` (it claimed "quoted as login_message quotes it"). `SAFE_PATH` is gone. The inline framing (`To run `, `: `, `, or `) stays: it is f-string text with no name. Scratch run against the real file: `runs=[every play that changed or failed since it ran here]`, `command=[<root>/run.bash --changed]`. |
| A15 | `00211-*/deploy.bash` `spike_passed`. It imports `spike_probe.VERDICT_PASS` (with PYTHONPATH set to the repo root, as spike.bash runs it) and fails if the value is empty or cannot be read. The `grep -qxF` now uses that value.                                                                                                                                                                                                                                                                                                                                                                                                                                                       |

Tests: `test_m7_check.py` has a new `test_the_headings_and_team_kind_are_read_from_the_go_table`, which covers the real values, a moved heading and kind being followed, and four malformed sources raising. The other three files have no unit tests.

## Task 2: the UNSURE rows

| Row                                                             | Ruling           | Reason, fix                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --------------------------------------------------------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00198 acceptance.bash:47-48                                     | NOT              | `DBusActivatable=false` and `Exec=... --new-window %U` appear in play-gsettings.yml only inline, as `regex_replace` arguments. They are the outcome the plan requires.                                                                                                                                                                                                                                                                                                                                      |
| 00144 acceptance.bash:93, 102                                   | NOT              | The extension is deployed as a whole-directory copy, with no file list in the product. `container-watch.timer` is inline in play-container-watch.yml (98, 150) and is also the unit's file name. Nothing names it.                                                                                                                                                                                                                                                                                          |
| 00109 acceptance.bash:116-139                                   | INSTANCE, partly | `HEALTH_UNIT` and `COLLECT_TIMER` are play vars (`unit_name`, `collect_timer` in play-host-health-login-report.yml), and `EXT_UUID` is `extension_name` in play-fedora-desktop-panel.yml. New `play_var` (it assigns, like `read_field`, so `abort` stays in the caller) parses the one `    name: plain-value` line or aborts. EXT_FILES, HEALTH_TARGET and RECOVERY\_\* are inline in tasks, so they are NOT. DOC_SECTIONS stays a deliberate spec (search category c).                                   |
| 00161 deploy_check.py:31 REMEDY                                 | NOT              | It is the checker's own advice. It names deploy.bash's `--bus-address=` flag, which is plan-local. No product holds it.                                                                                                                                                                                                                                                                                                                                                                                     |
| 00161 \_acceptance-u23.inc.bash:59; 00213 acceptance.bash:83-89 | INSTANCE, partly | The kit directory is named in `helpers/agent_bus/admin.py` `KIT_DIR`. u23: new `u23_kit_parent` reads it and requires `<abs parent>/kit`, because the README steps in the guest spell `kit`. It feeds both `tar -C` and the guest prepare. `U23_KIT_PARENT` is gone. 00213: `KIT_BIN` and `KIT_SETTINGS` are built from the KIT_DIR it read, and it fails fast if the read fails. The `/opt/claude-yolo/optional/agent-bus/*` paths and `/usr/local/bin/pingbus` are inline in play tasks, so they are NOT. |
| 00201 \_acceptance-m7.inc.bash:39-41, 188                       | INSTANCE         | These are `seat.SESSION_HOME`, `seat.SESSION_SETTINGS`, `membership.CCY_MOUNT`, and also `membership.DESIRED_FILE` (`membership.json` at :190). New `m7_check session-paths` prints them. `m7_prerequisites` reads them first, and `m7_seat_listing` refuses to run before they are read, so teardown cannot match nothing without saying so. Test: `test_the_session_paths_are_seat_and_membership_s`.                                                                                                     |

## Task 4: the (b) rows

Each value below exists only as inline text, except where marked REFUTED:

- **A4, A5:** inline. The refusal is a `raise UsageError(...)` literal (cli.py:1026). The "no verified team record yet" text is inline in an f-string in cli.py:995, inbox.py:334 and roles.go:263.
- **B2:** inline. `fmt.Fprintf` in choose.go:160.
- **B4:** mostly inline. `SENT` and `ABOUT` are keys of `cli.LINES`, a spec table keyed by the tag itself, so no other value exists to read. `TEAM`, `SEAT` and `trust=trusted` are inline at cli.py:2575-2579.
- **B5:** inline. A here-doc line of the launcher (:3461). Its keys are shell variable names.
- **B7:** REFUTED in part. "relaunches once", "relaunched once" and the "re-baseline" step name are inline (add.go:427/444, remove.go:120). The `✓`/`✗` step marks on the same lines are hot.go's `markDone`/`markFailed`. Fixed in M3: new `m7_check hot-marks` and `status-line-names`. `m3_read_names` is the first M3 leg and fills `M3_MARK_DONE`, `M3_MARK_FAILED`, `M3_PB_HOME` (the `/tmp/pingbus-home` copies at :265 and :315), `M3_SEG_HEAD` (`👥 `, statusline.go `label`) and `M3_INNER_VAR` (statusline.go `innerVar`). Its greps use `grep -P "^\Q<mark>\E ..."`.
- **B8 to B12, B14, B15:** inline. I grepped each literal in the product. Every hit is a message inside a call, an f-string or a heredoc, and none is assigned to a name.
- **B9:** REFUTED, already handled. `DISCONNECT_TEXT` is named (cli.py:2851), as the search said, but its checker (goodbye_check.py) is A7/A8's file and was outside this dispatch.

## Task 3: tests/ and scripts/

Method: two scratch scans under untracked/scratch/ (not tracked).

1. Module constants in tests/ and scripts/ that the product assigns with the same name or value: 56 hits.
2. Every product named string constant of 10 or more characters (helpers, files, Go) found as a literal in a test or script file that does not name the constant: 1076 hits.

I also grepped the test files for their own `*_RE`/`*_PATTERN` constants and for copy-describing comments, and read the candidates.

Ruling rule: a unit test that states an interface name or text as the thing it asserts is the specification (category c, NOT). That covers env var names, flags, file names, D-Bus names, section ids, versioned record headers in fixtures, and expected messages. A rename has to fail the test and be updated on purpose. An INSTANCE is a test that copies a product value into its own plumbing: the case only means what it says if that value equals the product's.

Instances found and fixed:

- `tests/helpers/ccy_lifecycle/test_plugin.py:574` `PLUGIN_NAME = "ccy-lifecycle"`. It is now `load_plugin_module().PLUGIN_NAME`.
- `tests/helpers/pingbus/test_cli_status.py:33` `TEAM_RE` copied `protocol.TEAM_NAME_PATTERN`, in a file that already used `protocol.HANDLE_PATTERN`. It is now that constant.
- `scripts/test-ccy-relabel-preflight.bash:106` `LABEL=...container_file_t:s0`, the "already shared" label the stub returns. The driver now prints `$CCY_SHARED_FILE_LABEL` from the common.bash it sources.
- `scripts/test-ccy-tmux-targets.bash:75` and `:243` re-set `CCY_TMUX_SOCKET=ccy` after sourcing tmux-session.bash, which sets it. Both are removed, and the comment now says the library's value is used.

NOT, by the rule above (sampled and read): fixture kernels, IPs and nonces, the fake homeserver's Matrix event types and paths (an external spec), play paths used as fixture input, `RESERVED_TLD`, colour codes in non-checker scripts, the `qa-*.tsv` path globs, ssh's `Enter passphrase for` prompt, and the 1076-hit interface-name class. I did not read all 1076 one by one. The plumbing form above was looked for in every test file whose hits were not plain interface names.

## Found, not fixed (outside the rows; same class)

- `00203-*/_acceptance-m4.inc.bash`: `✓`/`✗` marks at 238-366 (about 13 sites), `/tmp/pingbus-home` and `PINGBUS_STATUS_LINE_INNER` at 306, and `ccy/membership/.../membership.json` at 109.
- `00203-*/_acceptance-m6.inc.bash`: marks at 281-387, and `membership.json` at 94.
- `00203-*/m2-check.bash:25`: `HOME_IN=/tmp/pingbus-home`.
- `00203-*/_triage-hs.inc.bash:150-171`: `/tmp/pingbus-home/watch.log`.

The same fix applies to each: the m7_check commands added here (`session-paths`, `hot-marks`, `status-line-names`). I left them because they are about 25 call sites in slices this dispatch did not name.

## Verification

- `test_m7_check`: 52 OK. 00203 `test_m2_procs`, `test_m2_inbound` and `test_hs_journal`: 40 OK. 00161 `test_u23_kit_unpack` and `test_deploy_check`: 22 OK. `tests.helpers.ccy_lifecycle.test_plugin` and `tests.helpers.pingbus.test_cli_status`: 85 OK.
- `test-ccy-relabel-preflight.bash`: 60 passed, including "foreign entries already carrying the shared label". `test-ccy-tmux-targets.bash`: 23 passed.
- Every new product read was run once against this checkout: session-paths, hot-marks, status-line-names, admin.KIT_DIR, VERDICT_PASS, the removal words, the login words, and the play_var greps.
- shellcheck is clean on every touched bash file, including the sourced incs through their entry scripts.
- `./scripts/qa-changed.bash` passed (15 changed files).
- Not run: any acceptance or deploy script end to end. They are host-only.
