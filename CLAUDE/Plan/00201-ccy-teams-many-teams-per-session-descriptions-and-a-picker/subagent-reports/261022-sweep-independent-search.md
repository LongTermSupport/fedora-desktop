# Independent search: plan checkers holding a literal their subject product owns (Task 2.22 sweep)

The dispatch did not contain the rule or its pattern; I searched from the class description alone. I did not open
tests/helpers/plan_acceptance/, .semgrep/, src-go/qa/, any detector config, environment/ or vault files, and ran no detector or test.
For the QA tool as a product I cited only helpers/qa_environment/verdicts.py.

Technique key: T = found by text search (grep of the checkers, then of the product for the owner); R = found by reading a checker body.
Classes: (a) copy of a named product constant; (b) copy of inline product text; (c) specification, NOT an instance; (d) fixture, NOT an instance.
"Named" = the product defines it as a constant or variable; "inline" = only text inside a call or a function body.
Paths are under CLAUDE/Plan/ unless they start helpers/, files/, src-go/ or playbooks/. Line numbers are those of this worktree.

## State of the earlier search's rows in this tree

The tree has moved on since 261022-m3-owned-value-independent-search.md. Its rows A1, A2, A3, A6, A7, A8, A9, A10, A13, A17 no longer hold:
the checkers now read the product (m7_check.py description_max, seated_status_line, segment_label, go_string_const; goodbye_check.py
_checkout_module; deploy_check.py _IFACE_RE; triage_probe.py TUWUNEL_PIN; check-fetch-age.bash reads const FailedMark; m2_procs.py
lock_names_in; u20_check.py launch_choice_keys; _acceptance-m3 m3_read_names). They are counter-examples now (list at the end).
Still present from that report: A4 and A5 (m7_check.py:97 and :260; product text now at cli.py:1058 and :1027), B9's pane failures,
B6 and B7 (b19 and b22 below).

## Forms expected before searching

Session-home and mount paths retyped in a probe; file names inside a session home or membership directory; env var names; subcommand
and argv grammar matched against ps output; step marks and status marks in a regex; message texts in grep -q, *"..."*, in stderr;
wire-line tags and column numbers; container labels; unit names and state roots; file names of a state directory; regexes that copy
a grammar (team name); mktemp templates; computed copies (indent = width of a label, prefix + length).

## (a) Copies of a named or computed product value

| # | file:line | literal | product defines it at | verdict | tech |
| --- | --- | --- | --- | --- | --- |
| 1 | 00203-*/_acceptance-m4.inc.bash:306 | `-e PINGBUS_HOME=/tmp/pingbus-home -e PINGBUS_STATUS_LINE_INNER=1` | helpers/pingbus/seat.py:71 SESSION_HOME (named); src-go/internal/pingbus/statusline/statusline.go:31 innerVar (named) | INSTANCE, confident. The same plan's _acceptance-m3.inc.bash:61-69 reads both through m7_check (session-paths, status-line-names); M4 is the unmigrated copy | T, R |
| 2 | 00203-*/m2-check.bash:25 (used 124, 125, 132, 136, 146) | `HOME_IN=/tmp/pingbus-home`, with `active`, `settings.json`, `statusline-inner` appended | seat.py:71 SESSION_HOME; membership.py FLAG_FILE (seat.py ACTIVE_FLAG); seat.py SESSION_SETTINGS, STATUS_LINE_INNER (all named) | INSTANCE, confident. No citing comment on the path or the file names. The same file reads STATUS_LINE_GUARD from seat.py at :127, so it is half-migrated | T, R |
| 3 | 00203-*/m2-check.bash:24 (used 75-95, 116-123, 160) | `MOUNT=/run/ccy/membership`; `membership.json` | helpers/pingbus/membership.py CCY_MOUNT, DESIRED_FILE (named) | INSTANCE, confident | T |
| 4 | 00161-*/_acceptance-u20.inc.bash:155, 425, 1246 (file name 157, 1251) | `"/run/ccy/membership"` in podman --format templates; `membership.json` | membership.py CCY_MOUNT, DESIRED_FILE | INSTANCE, confident. Comments at :146-147 cite Plan 00203, not the constant. 00201 _acceptance-m7.inc.bash:157 reads both from m7_check session-paths (counter-example) | T |
| 5 | 00203-*/_acceptance-m4.inc.bash:109, 315; _acceptance-m2.inc.bash:200; 00161-*/_acceptance-u20.inc.bash:1123 | `${XDG_RUNTIME_DIR}/ccy/membership/<session>/membership.json` | src-go/internal/teams/membership/membership.go:104 filepath.Join(runtimeDir, "ccy", "membership", session) (inline); DESIRED_FILE named | INSTANCE, confident (directory inline, file name named) | T |
| 6 | 00203-*/_acceptance-m6.inc.bash:64 (used 94, 344, 429) | `${XDG_RUNTIME_DIR}/pingbus/membership/<session>` | membership.go:109 (inline) | INSTANCE, confident | T |
| 7 | 00203-*/_acceptance-m6.inc.bash:94, 248, 312, 324 | `applied.json` | membership.py APPLIED_FILE (named) | INSTANCE, confident | T |
| 8 | 00203-*/_acceptance-m6.inc.bash:344 | `relaunch.json` (negative: none was asked) | src-go/internal/teams/membership/desired.go:20 RelaunchFile (named); ccy_lifecycle.py:91 RELAUNCH_FILE | INSTANCE, confident | T |
| 9 | 00203-*/_acceptance-m6.inc.bash:54 | `${HOME}/.config/pingbus/element-accepted` | helpers/pingbus/config.py:67 ELEMENT_ACCEPTED_DIR, :68 ELEMENT_ACCEPTED_FILE; src-go/internal/teams/chooser/consent.go:28 (named) | INSTANCE, confident | T |
| 10 | 00203-*/_triage-hs.inc.bash:150-171 (4 uses); 00203-*/triage-hs.bash:30 (comment); 00161-*/_acceptance-u20.inc.bash:967; 00211-*/_acceptance-stage2.inc.bash:21 (CCY_PINGBUS_HOME) | `/tmp/pingbus-home/watch.log`, `/tmp/pingbus-home` | seat.py:71 SESSION_HOME; seat.py WATCH_LOG (named) | INSTANCE, confident. Comments describe the file, none cites the constant | T |
| 11 | 00201-*/_acceptance-m7.inc.bash:61 and :735; 00194-*/_acceptance-goodbye.inc.bash:40 | `/tmp/pingbus-sessions-{os.getuid()}` | helpers/pingbus/statusline.py:72 SESSIONS_DIR_TEMPLATE (named); membership.go:107 (Go) | INSTANCE, confident. The m7 comment at :60 cites "hooks.py's default directory"; hooks.py:210-211 only re-exports it, so the citation is stale | T |
| 12 | 00203-*/_acceptance-m2.inc.bash:64, m3:45-46, m4:46-47, m6:51-52; 00201-*/_acceptance-m7.inc.bash:28; 00161-*/_acceptance-u20.inc.bash (U20_BUS_DIR, U20_SEATS_DIR, U20_REPORTS); 00194-*/_acceptance-goodbye.inc.bash:81; 00209-*/_acceptance-s1.inc.bash:424, 489 | `.claude/ccy/pingbus/seats`, `.claude/ccy/pingbus/sessions`, the `seats/<team>/<seat>/member.json` layout | seat.py CHECKOUT_SEATS, SESSIONS_DIR (named) | INSTANCE, confident (about 12 sites, one value) | T |
| 13 | 00211-*/_acceptance-stage2.inc.bash:26-37 (WHERE_PY) | `inbox`, `consumed`, `dropped.log`, `<event>.json`, tab field index 1 | helpers/pingbus/inbox.py:52 INBOX_DIR, :53 CONSUMED_DIR, :63 DROPPED_LOG (named) | INSTANCE, confident. Runs in the guest under python3 -I -c, so a fix would pass the names in. File names sit inside an embedded Python string | R |
| 14 | 00211-*/_acceptance-stage2.inc.bash:107-109 | `${home}/.config/pingbus`, `/state` | config.py:139 (inline); config.py:37 STATE_DIR (named) | INSTANCE, confident | R |
| 15 | 00209-*/_acceptance-s1.inc.bash:412 | `^[a-z][a-z0-9-]{0,23}$` (team-name grammar) | helpers/pingbus/protocol.py:87 TEAM_NAME_PATTERN (named) | INSTANCE, confident | R |
| 16 | 00128-*/probe-gate-census.bash:42 | `^[✓✗⚠] [a-z0-9][a-z0-9-]*: ` | helpers/qa_environment/verdicts.py:43 STAGE (named regex); the comment above cites verdicts.py | INSTANCE (status marks plus grammar), confident | T |
| 17 | 00203-*/m2_procs.py:78-79; 00203-*/_acceptance-m4.inc.bash:152; _acceptance-m6.inc.bash:144; 00161-*/u20_check.py _watch_mode | `["seat", "keep"]`, `--launch`, `--parent`, `/pingbus/ && / seat keep/`, `watch`, `--supervise`, `--child`, `("pingbus", "agent-bus")` | src-go/internal/teams/hot/cc.go:112-114 keeperLine (inline " seat keep"); helpers/pingbus/cli.py:2170-2203 watch grammar (inline) | INSTANCE of the product's argv grammar, moderately confident. A renamed subcommand makes the keeper count 0 | R |
| 18 | 00203-*/_acceptance-m4.inc.bash:238, 239, 242, 251, 256, 284, 322, 330, 331, 346, 366; _acceptance-m6.inc.bash:281, 282, 341, 363, 387 | the mark `✓` (and `✗`) at the head of a step line | src-go/internal/teams/hot/hot.go:56-58 markDone, markFailed, markSkip (named) | INSTANCE, confident. M3 reads the marks via m7_check hot-marks; M4 and M6 retype them | T |
| 19 | 00203-*/_acceptance-m6.inc.bash:409, 412 | prefix `${XDG_RUNTIME_DIR}/cc-teams.`, length prefix + 6, alphanumerics | files/var/local/claude-code/cc:198 mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/cc-teams.XXXXXX" (inline) | INSTANCE, confident; a computed copy of a mktemp template (6 = the X count). No string to grep for | R |
| 20 | 00203-*/_acceptance-m6.inc.bash:359 | `unguarded: end it` | src-go/internal/teams/hot/cc.go:26 ccUnguarded (named) | INSTANCE, confident. m4:344-347 retypes the word `unguarded` of list.go:228 (inline) | T |
| 21 | 00161-*/u20_check.py:669-676; 00201-*/m7_check.py:736-737 | container label `ccy-seats` | no product hit: grep of helpers/, files/, src-go/internal/ finds no ccy-seats. SPEC-hot-control.md:221 says it is retired | INSTANCE of the negative form: the "no ccy-seats label" check cannot fail, and u20_check seat_containers reads a label no container carries. UNSURE: the check may deliberately assert the retirement | T (zero product hits), R |
| 22 | 00080-*/triage.bash:156, 202, 221 | labels `ccy-project`, `ccy-github`, `ccy=true`; name pattern `^.+_(yolo\|browser)(_[0-9]+)?$` | files/var/local/claude-yolo/claude-yolo:3883-3885 (inline --label), `_yolo` at :1164 | INSTANCE of inline values, moderately confident. The script says it counts legacy unlabelled sessions by name on purpose | T |
| 23 | 00203-*/triage-hs.bash:72-74 (STATE_ROOT, HEALTH_ROOT, TUWUNEL); _acceptance-m3.inc.bash:118; m4:77; _acceptance-robust.inc.bash:118, 126, 196; 00201-*/_acceptance-m7.inc.bash:32-34; 00194-*/_acceptance-goodbye.inc.bash:235-239, 750-751 | `/var/lib/agent-bus`, `/var/lib/agent-bus-applied/health`, `/var/lib/agent-bus-install`, `/var/lib/agent-bus-applied`, `/usr/local/lib/agent-bus/tuwunel`, `agent-bus-state-<id>.tar` | helpers/agent_bus/render.py:26 STATE_ROOT; health.py:48 HEALTH_DIR, :50 INSTALL_STATE_ROOT; checkout.py:60 APPLIED_DIR (named); agent-bus-state- inline at agent-bus-install:1006 and health.py:331; tuwunel path inline at agent-bus-install:1091 | INSTANCE, confident for the named ones | T |
| 24 | 00203-*/_acceptance-robust.inc.bash:33-34; _triage-hs.inc.bash:27-30, 49, 199-200; hs_journal.py:39, 353, 420, 478-480; m4:76, 299, 316 | `agent-bus-hs@<team>.service`, `agent-bus-hs-health@`, `agent-bus-backup@` | files/usr/local/sbin/agent-bus-install:46-47 UNIT_FILES (named) | INSTANCE of a unit-name family; low severity, moderately confident | T |
| 25 | 00137-*/acceptance.bash:80-85 | `/usr/local/sbin/fedora-desktop-self-update`, `/etc/fedora-desktop`, `/var/lib/fedora-desktop/deploy`, `.../self-update-status`, `.../ansible-collections` | playbooks/imports/optional/common/play-self-update.yml:27, 31, 42 (vars); helpers/self_update/published.py:25 DIRECTORY (named); the sbin script :44, 52, 55 | INSTANCE, moderately confident; the gate also states these as what the host must hold, a shade of (c) | T |
| 26 | 00148-*/acceptance.bash:112, 119 | gsettings keys `stop-grace-seconds`, `server-idle-timeout-minutes`, `max-recording-minutes`, `silence-autostop-seconds`, `dictation-paste-interval-seconds`, `continuous-dictation`, `server-start-at-login` | extensions/speech-to-text@fedora-desktop/schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml (e.g. :99) | UNSURE: the keys are the product's, but asking the reader for each also states what must exist | T |
| 27 | 00109-*/acceptance.bash:119, 136-139, 923 | EXT_FILES, RECOVERY_DIR/RULE/DOCK_UNIT/SUSPEND_UNIT, `REFRESH_BACKGROUND` | extensions/ files; helpers/displaylink_recovery/recovery.py:64 Action.REFRESH_BACKGROUND (named) | UNSURE (what must be deployed). The same file reads EXT_UUID, HEALTH_UNIT and COLLECT_TIMER from the plays (play_var, :224-226) and the document keys from the producer (:297-330) | T |
| 28 | 00194-*/_acceptance-goodbye.inc.bash:34 | `GB_REASON="exit"` | helpers/pingbus/hooks.py:85 GOODBYE_REASONS["prompt_input_exit"] (named) | INSTANCE; the comment cites the SessionEnd reason, not the table. Confident | T |
| 29 | 00201-*/m7_check.py:537 | guard.startswith("[ -e "), endswith(f"exec {kit}") | seat.py:67-68 STATUS_LINE_GUARD (named) | INSTANCE of the guard's shape (the head), not its value; the rest of the function reads the product. UNSURE | R |
| 30 | 00201-*/m7_check.py:95 _SEGMENT_MARKS | `(?: \([a-z]+\))*`, the form of ` (down)` | statusline.py:462 and statusline.go:379 (inline) | INSTANCE of an inline form; UNSURE (the comment states it deliberately) | R |
| 31 | 00194-*/goodbye_check.py:73; 00161-*/u20_check.py:688 _CONF_RE | `LAST_TOKEN=` record line | files/var/local/claude-yolo/claude-yolo:3458 (inline) | INSTANCE (record format), moderately confident | T |
| 32 | 00203-*/_acceptance-m2.inc.bash:41, 77, 150, 196; _acceptance-m3.inc.bash:47; 00161-*/_acceptance-u20.inc.bash:1141 | label `ccy-session=<name>`; directory `.claude/ccy/projects/-workspace` | files/var/local/claude-yolo/lib/agent-bus-membership.bash:54 (inline); lib/session-registry.bash:1018 (inline in a case branch) | INSTANCE, moderately confident | T |
| 33 | 00201-*/m7_check.py:777; 00203-*/m2-check.bash:17, 158, 168; 00213-*/_acceptance.inc.bash:228 | env var names `PINGBUS_MEMBERSHIP`, `off`, the `PINGBUS_` prefix (a negative check), `PINGBUS_TEAMS`, `PINGBUS_HOME` in a clearing list | membership.py MEMBERSHIP_VAR, MEMBERSHIP_OFF (named); config.py | INSTANCE, confident for m7_check:777 (negative) and m2-check; 00213's unset list leaves a renamed variable set | R, T |
| 34 | 00161-*/triage_probe.py:204 | `tuwunel_version:` pin-file key | files/usr/local/share/agent-bus/tuwunel.pin; declare.py PIN_FILE parse | INSTANCE, low severity | T |

## (b) Copies of inline product text

Matched with grep -q, grep -F, a `*"..."*` test or `in stderr`. A reworded message makes a correct product fail; N marks a negative check
(it passes a wrong product when the text drifts).

| # | file:line | literal | product location | N | tech |
| --- | --- | --- | --- | --- | --- |
| b1 | 00203-*/_acceptance-m4.inc.bash:238 | `role: [^ ]+ set to coordinator` | src-go/internal/teams/roles/roles.go:243 said(st.Handle + " set to " + st.To + st.Note) | | T |
| b2 | m4:239 | `session: told it is <team>'s coordinator` | hot/role.go:244-247 roleWords; :260, :264 | | T |
| b3 | m4:242, 256 | `^✓ (role\|record\|membership)` | hot.go:602 "membership: "+words; role.go:57 "role: "+line | N | T |
| b4 | m4:251, 330 | `^✓ goodbye: ` | hot/remove.go (the goodbye step) | | T |
| b5 | m4:284, 365, 366 | `^✓ membership: generation`, `session ended during the add`, `^✓ membership: removed` | hot/add.go:388; hot.go:602, :612 | | T |
| b6 | m4:285 | `another change to it is in progress: ccy-teams session add` | hot.go:455 | | T |
| b7 | m4:319 | `homeserver for <team> is down` | hot/add.go:108 | | T |
| b8 | m4:324 | `goodbye and status not sent: homeserver down` | hot/remove.go:128 | | T |
| b9 | m4:331 | `written \(farewell done\)$` | remove.go:139 passes "farewell done"; `written (...)` comes from the formatter | | T, R |
| b10 | m4:346 | `^✓ keeper: running again, 1 seat held` | hot/list.go:414 | | T |
| b11 | m4:309 | `<team>(down)` | statusline.go:379; statusline.py:462 | | T |
| b12 | m4:344, 347 | list STATE words `unguarded`, `ok` | hot/list.go:228; hot.go:121 | | T |
| b13 | 00203-*/_acceptance-m6.inc.bash:281-283 | `session <n>: running, can be hot-controlled`; `route: this host reaches <t>'s homeserver`; `Moved: <s> is in <t>, and no longer in <u>.` | hot.go:471; cc.go:120; hot/move.go:29 | | T |
| b14 | m6:341-343, 349 | `session: <s> is in <t> as seat N, but it started on no team`, `and cc has nothing that can restart it`, `Nothing restarted` | hot/cc.go:130-131; add.go:341 | N for `Nothing restarted` | T |
| b15 | m6:353 | `Nothing to do: <s> is already in <t> as seat <n>.` | add.go:490 | | T |
| b16 | m6:361, 363 | `is a cc session: its seat keeper ends with its claude`; `record: no --teams \(cc keeps --bus\)` | hot/cc.go:145; hot.go:576 | | T |
| b17 | m6:387 | `it was started without --teams or --bus` | not found verbatim in hot/ by my grep (UNSURE where it is built) | | T |
| b18 | m6:389 | `named by session <name>` (must not appear) | hot/add.go:165; helpers/agent_bus/checkout.py:737 | N | T |
| b19 | 00203-*/_acceptance-m3.inc.bash:249 | `relaunches once\|relaunched once` (must not appear) | hot/add.go:427, 444; hot.go:132 | N | T |
| b20 | m3:279, 321 | `you joined team <t>`, `you left team <t>` | helpers/pingbus/hooks.py:155-156 TEMPLATES["joined"], ["left"] (named, importable; m3 already imports the farewell text from cli) | | T |
| b21 | m3:280, 290 | `kind: MEMBERSHIP`, `kind: HUMAN` | helpers/pingbus/envelope.py:62, 66 KIND_LINES (named) | | T |
| b22 | m3:320 | `re-baseline` step | hot/remove.go:120 | | T |
| b23 | m3:254 | step line `seat <name>: free` (the seat name is extracted from it) | hot/role.go:234-238 "seat "+seat+": ..."; the `free` part not located | | R |
| b24 | m3:295; m4:127, 143, 338; m6:108, 123 | wire tags `SEAT`, `MEMBER`, `SESSION`, column numbers 3-6, `free`/`held` | seat.py LINE_KIND, HELD, FREE (named); list.go (SESSION line) | | R |
| b25 | 00203-*/_acceptance-robust.inc.bash:187, 199, 205, 207, 232, 233 | `PROBLEM systemd restarted the homeserver`, `the newest backup is 3 d`, `health: `, `is starting`, `is up after` | helpers/agent_bus/health.py:198, 185, 256; checkout.py:687, 693; src-go/internal/teams/board/health.go:37-56 | | T |
| b26 | 00203-*/_triage-hs.inc.bash:164-167 | `the watcher exits`, `the watcher ended`, `the watcher was refused`, `starting it again`, `no longer watched`, `does not answer` | helpers/pingbus/cli.py:2182-2192, 2283-2335, 2395, 1833 (inline f-strings) | counts: drift gives a zero, not a failure | T |
| b27 | 00203-*/m2-check.bash:198 | grep -q 'PINGBUS_MEMBERSHIP-' entrypoint.sh | files/var/local/claude-yolo/entrypoint.sh (variable found; exact expansion shape not verified) | | R |
| b28 | 00167-*/check-cc-project-dir.bash:35 | `is not in a git repository`; `[Y/n]` (must not appear) | src-go/internal/ccproject/app.go:147, 186, 168 (inline) | N | T |
| b29 | 00201-*/m7_check.py:97, :260 | CHOICES_TEXT; `no verified team record yet` | cli.py:1058, :1027 (inline) | | T |
| b30 | 00194-*/goodbye_check.py:60-64, 68 | `pingbus seat disconnected failed`, `... was stopped`, `ERROR:` | files/var/local/claude-yolo/lib/agent-bus-membership.bash:111-113; lib/common-pure.bash:26 | N | T |
| b31 | 00161-*/_acceptance-u20.inc.bash:941-943 | `is held by another session`, `one seat per team per session`, `trailing comma` | seat.py:109, 213, 184 | | T |
| b32 | 00183-*/check-fetch-age.bash:125-129 | `" [fetched 3d ago]"`, `" [no fetch recorded]"` | src-go/internal/fetchage/fetchage.go:89 and the format near :14 (inline); FailedMark is read from source | | T |
| b33 | 00211-*/_acceptance-stage2.inc.bash:825 | `a host member must not run as a user with an Element profile` | helpers/pingbus/config.py:447; src-go/internal/pingbus/statusline/roles.go:250 | | T |
| b34 | 00061-*/acceptance.bash:75, 110 | `"provisioning_profile": "(desktop\|server)"`, `is not recognised` | run.bash (not located in depth) | | T |

## (c) Specification statements, NOT instances

- 00203-*/m2_inbound.py:28-30 WANT = "refuse", crossSessionInbound, --settings, claude/claude.exe: Claude Code's key and argv (external); WANT is Decision 1.
- 00203-*/_acceptance-robust.inc.bash:149 Type=notify, WatchdogUSec == 30s, Restart=always: the plan's R1 requirement, judged against systemd's answer.
- All throwaway names (acc-robust, acc-m3a, acc-m4a, accept194a) and bounds; M2_OWN_NAMES, U20_OWN_NAMES: the plans' own fixtures.
- 00203-*/hs_journal.py classification of journal lines (Watchdog timeout, Main process exited): systemd's messages and the tool's own judgement.
- 00109-*/acceptance.bash VMTEST_SCENARIO, graphical.target: the plan's required outcome.
- 00161-*/u20_check.py:112-190 STOP_LINE and _ORDERS: instructions the plan writes to a model, and the hooks daemon's rule.
- 00212-*/check-media-volume.bash and fake-*.py strings that the fakes print themselves.

## (d) Fixtures, NOT instances

- 00211-*/acceptance.bash:186-191 (TWO, GONE, TEAMS2, SENT_LINE, HISTORY_TWO, HISTORY_ONE) and stage2 :663-683 (fake PROBLEM, MEMBER, ROOM, HUMAN lines). They repeat the wire shape, so a wire change makes the fixtures stale.
- 00194-*/_acceptance-goodbye.inc.bash:799-838 (GB_FAKE_*, fake panes).
- 00192-*/qa-all-golden/capture.py:77-199, qa-changed-golden/capture.py, golden-capture/capture.py: stub outputs and REPORT_ASSIGN = 'JSON_OUT="/tmp/qa-results.json"', a rewrite of legacy qa-all.bash at a pinned git revision.
- test_*.py beside the checkers (00161, 00194, 00201, 00203): unit tests of the checkers; not read in depth.
- 00209-*/_acceptance-s1.inc.bash:381-393 fixture_member.

## Counter-examples (the checker reads the product)

- 00203 _acceptance-m3.inc.bash:61-94 m3_read_names, :298-306 m3_removal_words; m2_procs.py lock_names_in; m2-check.bash:127 (STATUS_LINE_GUARD), :195 (REQUIRED_CONTAINER_VERSION).
- 00201 m7_check.py description_max, seated_status_line, segment_label, segment_piece, go_string_const, hot_marks, status_line_names; _acceptance-m7.inc.bash:157.
- 00109 acceptance.bash:297-330 and :224-226.
- 00183 check-fetch-age.bash:34-47.
- 00161 u20_check.py notice_re(envelope.FIRST_LINE), launch_choice_keys; deploy_check.py _IFACE_RE; triage_probe.py TUWUNEL_PIN.
- 00194 goodbye_check.py:48-58 (TEXT_KEY, GOODBYE_TEXT, DISCONNECT_TEXT imported).

## Counts

| Class | Count |
| --- | --- |
| (a) rows 1-34 | 34 (confident: 1-16, 18-20, 23, 28, 33; moderate or unsure: 17, 21, 22, 24-27, 29-32, 34) |
| (b) rows b1-b34 | 34 (b17, b27, b34 not fully located) |
| Negative checks among them | rows 8, 21, 33; b3, b14, b18, b19, b28, b30 |
| (c) groups | 7 |
| (d) groups | 5 |

## What text search found that reading could not have checked

- Every "product defines it at" location and its named or inline status: only a grep of helpers/, files/, src-go/internal/, extensions/ and run.bash for the literal shows whether a constant exists.
- The absence of a counterpart: row 21 (ccy-seats is nowhere in the product).
- That earlier rows had been fixed (grep of the old constant names in the current checkers).
- The spread of one value across plans: /tmp/pingbus-home (row 10), /tmp/pingbus-sessions- (row 11) and .claude/ccy/pingbus/seats (row 12) each occur in 3 to 12 files.

## What reading found that text search could not have

- Row 13 (file names inside an embedded python3 -c string) and row 14 (a path built across a function).
- Row 19: the mktemp template is a prefix plus a length test, "${#home}" -ne $((${#prefix} + 6)); no string to grep for.
- Row 15: the team-name grammar retyped as a bash regex.
- Row 17: the keeper and watch argv grammar spread over a list slice and flag lists.
- Rows 18 and 20: the same product mark retyped in M4 and M6 while M3 reads it; only reading M3's header with M4's body shows the inconsistency.
- Rows 29, 30: shape copies next to code that does read the product.
- Which pairs belong in (c) and (d) (comments in robust, 00109 and 00211) and which checkers are counter-examples.

## Searches run

- Listing of every .bash, .py, .sh under non-Completed, non-Cancelled, non-Archive plan folders.
- Greps over the checkers, then the product: pingbus-home, /run/ccy, ccy/membership, pingbus/membership, keeper.lock, watch.log, seat.lock, said-goodbye, membership.json, applied.json, told., statusline-inner, member.json, PINGBUS_, CCY_, settings.json, ccy/pingbus, pingbus-sessions, /var/lib/agent-bus, ccy-seats, ccy-session=, ccy-project, projects/-workspace, relaunch.json, element-accepted, cc-teams., arg=, seat keep, --supervise, --settings.
- Message greps in the product for each literal found in a grep -q/-F/-E, `*"..."*` or `in stderr` test, in src-go/internal/teams, helpers/pingbus, helpers/agent_bus, files/var/local.
- Pattern greps over all checkers: grep with a quoted literal, =~, == *\*, readonly X=/usr|/var|/etc|/opt|~, startswith(, re.compile, module-level constants.
- Reading in full: 00203 _acceptance-m1, m2, m3, m4, m6, robust, _triage-hs, m2-check.bash, m2_procs.py, m2_inbound.py; 00183 check-fetch-age.bash; 00109 acceptance.bash (probe and head); 00161 u20_check.py (constants, orders, containers, launch choices); 00201 m7_check.py (constants, Go-source readers, settings checks); 00194 goodbye_check.py (head, panes); 00211 stage2 (head, session helpers); 00209 s1 (parts); 00128 probe-gate-census.bash; 00167 check-cc-project-dir.bash.

## Not examined in depth, not claimed clean

00161 triage_probe.py, u01_probe.py and acceptance_check.py beyond constants and `in stderr` tests; 00194 _acceptance-goodbye.inc.bash bodies past the constants; 00201 _acceptance-m7.inc.bash bodies beyond the lines listed; hs_journal.py report text; 00092 probe-invariant, probe-functional, probe-host; 00098, 00139, 00143, 00149, 00185 probes (system state; a path grep found no repo-owned value); deploy.bash files of 00054, 00058, 00158, 00160, 00165, 00173, 00196 (grepped for paths and unit names only); 00170 unifi scripts.
