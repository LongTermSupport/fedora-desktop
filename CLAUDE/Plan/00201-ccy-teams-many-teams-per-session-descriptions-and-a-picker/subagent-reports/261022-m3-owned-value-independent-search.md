# Independent search: checkers that hard-code a value their subject owns

The dispatch did not contain the rule or its pattern; I searched from the class description alone. I did not open
tests/helpers/plan_acceptance/, src-go/qa/, .semgrep/, any detector config, vault or host_vars, and did not run the detector.

Scope: every non-Completed, non-Cancelled folder under CLAUDE/Plan/ (acceptance*, _acceptance*.inc, triage*, probe*, *_check.py,
check-*.bash, deploy_check.py). Subject locations checked: helpers/, files/, src-go/internal/, playbooks/, extensions/, run.bash.

Categories: (a) copy of a named constant (or computed value) the subject owns; (b) copy of inline message/command text in the subject;
(c) a value the checker states as the specification, NOT-AN-INSTANCE. T = found by text search, R = found by reading.

## Forms expected before searching

Constants re-declared with a "#: see helpers/..." comment; startswith/endswith/== on a command string; grep -F/-q on message text;
regexes rebuilt from a template; file/path/unit-name lists; record key lists; lock/flag file names; version pins; D-Bus names; column
headings; wire-line tags; a re-implemented product function; stale copies where the product has since moved.

## Counts

| Category | Count |
| --- | --- |
| (a) named/computed value copied (A1-A19 less A4, A5) | 17 |
| (b) inline message/command text copied (A4, A5, B2-B15) | 16 |
| (c) spec, NOT-AN-INSTANCE | 8 |
| UNSURE | 6 |
| Counter-examples (checker reads the product) | 5 |

Confirmed drifted today: 3 rows, 2 causes. A1 and A19 (status-line guard tail), and A9 (Tuwunel pin v1.9.3 vs v1.9.4).

## (a) Named or computed values the subject owns

Paths are under CLAUDE/Plan/ unless they start helpers/, files/, src-go/ or playbooks/.

| # | file:line | literal | subject defines it at | verdict | found by |
| --- | --- | --- | --- | --- | --- |
| A1 | 00201-*/m7_check.py:438 (used 457-460) | GUARD_TAIL = "exec pingbus status-line" | helpers/pingbus/seat.py:67-68 STATUS_LINE_GUARD, tail now `exec pingbus-status-line` | INSTANCE, DRIFTED. The originating instance, still unfixed in this tree. The comment above it names STATUS_LINE_GUARD. The module already imports helpers.agent_bus.declare lazily, so it could import seat | T and R |
| A2 | 00201-*/m7_check.py:67 | DESCRIPTION_MAX = 120 | helpers/pingbus/protocol.py:164 | INSTANCE (comment cites it) | T |
| A3 | 00201-*/m7_check.py:77-78 | SEGMENT_LABEL = "👥 ", SEGMENT_INDENT = "   " | helpers/pingbus/statusline.py:75 LABEL; indent computed at statusline.py:352 as display_width(f"{LABEL} ") | INSTANCE. The 3-space indent is a computed value copied as a literal | R (indent), T (label) |
| A6 | 00203-*/m2_procs.py:30 (used 89) | LOCK_NAMES = ("keeper.lock", "seat.lock") | helpers/pingbus/seat.py:48 KEEPER_LOCK, seat.py:76 LOCK_FILE | INSTANCE. The file runs in a container under `python3 -I` with no checkout, so a fix would pass the names in | T |
| A7 | 00194-*/goodbye_check.py:35 | TEXT_KEY = "agent_bus.text" | helpers/pingbus/protocol.py:40 | INSTANCE (comment cites it) | T |
| A8 | 00194-*/goodbye_check.py:37 | GOODBYE_TEXT = "{handle} left the team: session ended ({reason})" | helpers/pingbus/hooks.py:128 TEMPLATES["goodbye"] | INSTANCE (importable dict entry) | T |
| A9 | 00161-*/triage_probe.py:59 (default at 1710) | TUWUNEL_VERSION = "v1.9.3" | files/usr/local/share/agent-bus/tuwunel.pin:9 `tuwunel_version: v1.9.4` (declare.py:475 PIN_FILE) | INSTANCE, DRIFTED. Mitigation: a triage that records the version it ran; `--tuwunel-version` overrides | T |
| A10 | 00161-*/deploy_check.py:28 (used 63) | BUS_IFACE = "agentbus0" | helpers/agent_bus/checkout.py:56 BUS_IFACE; files/usr/local/sbin/agent-bus-install:40 | INSTANCE. Plainest copy: same name, same value | T |
| A11 | 00161-*/triage_probe.py:57 | PREFIX = "agent_bus" | helpers/pingbus/protocol.py:36 | INSTANCE | T |
| A12 | 00161-*/triage_probe.py:172-195 | team_power_levels() dict | helpers/pingbus/protocol.py:1129 expected_power_levels() | INSTANCE. Re-implements a product function; equal today. team_record() (about line 206) lacks keys protocol.TEAM_KEYS (line 152) now lists (org, exclude): UNSURE if the product refuses it | R |
| A13 | 00183-*/check-fetch-age.bash:28 | FAILED_MARK=' [fetch failed or running]' | src-go/internal/fetchage/fetchage.go:60 const FailedMark | INSTANCE (bash cannot import a Go constant) | T |
| A14 | 00055-*/acceptance.bash:14, 579, 631, 651 | `FindingsChanged`, `org.fedoradesktop.ContainerWatch` | helpers/containerwatch/cli.py:51-53 DBUS_PATH, DBUS_INTERFACE, DBUS_SIGNAL | INSTANCE | T |
| A15 | 00211-*/deploy.bash:104 | 'Verdict: every fact holds' | plan-local spike_probe.py:51 VERDICT_PASS | INSTANCE of the class, but the subject is a plan script, not product | T |
| A16 | 00161-*/u20_check.py:130-134 NOTICE_RE | tail `: N messages? (N from humans, N from agents, N pings, N timeouts)` | helpers/pingbus/envelope.py:83 FIRST_LINE (head read via `.split(":", 1)[0]`, tail retyped) | INSTANCE, partial: half read, half copied | R |
| A17 | 00161-*/u20_check.py:673 | _LAUNCH_CHOICE_KEYS = ("LAST_TOKEN", "LAST_SSH_KEYS", "LAST_NETWORK") | files/var/local/claude-yolo/claude-yolo:569 loop over `LAST_TOKEN LAST_SSH_KEYS LAST_NETWORK LAST_TEAMS` | INSTANCE (LAST_TEAMS already differs; may be deliberate) | R |
| A18 | 00203-*/m2-check.bash:118 | jq startswith of the guard head `[ -e "$PINGBUS_HOME/active" ] \|\| exec sh` | seat.py:67 STATUS_LINE_GUARD head (ACTIVE_FLAG, STATUS_LINE_INNER) | INSTANCE (head copy; matches today) | T |
| A19 | 00201-*/test_m7_check.py:425 | whole guard string as a fixture, old tail | seat.py:67-68 | INSTANCE, DRIFTED with A1. A unit test of the checker, so it keeps passing against the stale tail by design | T |

## (b) Inline message or command text copied

A4 and A5 are in m7_check.py; B2-B15 follow. No constant exists in the subject for these, so reading from source would need one.

| # | file:line | literal | subject |
| --- | --- | --- | --- |
| A4 | 00201-*/m7_check.py:82 (used 218-232, 429) | CHOICES_TEXT "several teams are active and --to/--re fit more than one (or none): name one with --team:" ("word for word") | helpers/pingbus/cli.py:1026 |
| A5 | 00201-*/m7_check.py:225-226 | "no verified team record yet" | helpers/pingbus/cli.py:995 |
| B2 | 00201-*/m7_check.py:366-369 | "This session is on N team(s):" and the seat line layout | src-go/internal/teams/choose/choose.go:160 |
| B3 | 00201-*/m7_check.py:351 | headings "MEMBER", "KIND", "STATE" | src-go/internal/teams/board/table.go:67 `headings` (named array) |
| B4 | 00201-*/m7_check.py:327, 396, 405, 418 | wire tags SEAT, ABOUT, SENT, TEAM, `trust=trusted` | helpers/pingbus/cli.py:109-111, 2575 |
| B5 | 00201-*/m7_check.py:195; 00161-*/u20_check.py:670 | `LAST_TEAMS="..."` record format | files/var/local/claude-yolo/claude-yolo:3461 |
| B6 | 00203-*/_acceptance-m3.inc.bash:269 | "removed by the owner" | helpers/pingbus/membership.py:48 FAREWELL_REASONS (named) |
| B7 | 00203-*/_acceptance-m3.inc.bash:211-215, 261, 293 | "relaunches once", "relaunched once", "re-baseline" step | src-go/internal/teams/hot/add.go:427, 444; remove.go |
| B8 | 00203-*/_acceptance-robust.inc.bash:187, 199-207, 232-233 | "PROBLEM systemd restarted the homeserver", "the newest backup is 3 d", "health: ", "is starting", "is up after" | helpers/agent_bus/health.py:198, 185, 256; helpers/agent_bus/checkout.py:687, 693 |
| B9 | 00194-*/goodbye_check.py:39, 42-46 | DISCONNECT_TEXT, "pingbus seat disconnected failed/was stopped", "ERROR:" | helpers/pingbus/cli.py:2851 (named); files/var/local/claude-yolo/lib/agent-bus-membership.bash:111-113; lib/common-pure.bash:26 |
| B10 | 00161-*/_acceptance-u20.inc.bash:941-943 | "is held by another session", "one seat per team per session", "trailing comma" | helpers/pingbus/seat.py:109, 213, 184 |
| B11 | 00171-*/probe-insert-sequence.py:30-37 | "Insert does nothing while ", "INSERT KEY PRESSED", "Started X recorder N (session", "Insert while RECORDING: stop after the grace", "Dropped", "Display: " | extensions/speech-to-text@fedora-desktop/panelSession.js:127, 243, 432, 495; extension.js:215 |
| B12 | 00212-*/check-media-volume.bash:140-197 | "nothing is playing", "no stream of it is left", "a new stream of it", "parent process has ended" | src-go/internal/wsimedia/app.go:189, 438, 460, 392 |
| B13 | 00214-*/check-changed-plays.bash:22, 88, 105 | REMEDY 'To run every play that changed or failed since it ran here: ', ", or X to run every play" | helpers/host_health/login_message.py:88 `_RUNS_CHANGED` (named; f-strings at 265, 347) |
| B14 | 00063-*/acceptance.bash:79-125; 00073-*/acceptance.bash:167-192; 00074-*/acceptance.bash:79-101 | run.bash messages ("is not a valid email", "Multiple ... accounts", "is not a readable file", "sudo said:", "No legacy cgroup configuration found", "removed successfully", "still present after removal") | run.bash:298, 311, 139, 263, 196, 223, 220 |
| B15 | 00169-*/acceptance.bash:230, 240; 00192-*/acceptance-rclone-cache-status.bash:109-118; 00210-*/check-convert.bash:46; 00137-*/acceptance.bash:371 | "cannot read /workspace", "reads /workspace", "No rclone mounts found.", "cache: ", "it can become: docx", `SELF-UPDATE-CYCLE (nothing\|dry-run)` | files/var/local/claude-yolo/entrypoint.sh:53, 62; src-go/internal/rclonecache/app.go:191, 310, 324; src-go/internal/convert/app.go:156; helpers/self_update/cycle.py:401, 430 |

Several are the message contract the plan itself asserts; they carry the same hazard (a reworded message fails a correct product) at lower severity.
All were found by T (grep of the subject for the literal); the layout and heading ones (B2, B3) also needed R.

## (c) Specification statements, NOT-AN-INSTANCE

| file:line | literal | why |
| --- | --- | --- |
| 00213-*/acceptance.bash:83-89, _acceptance.inc.bash:68 | `pingbus-status-line` as statusLine.command | The plan's own deliverable; the checker reads the deployed files and holds them to the value the plan sets |
| 00203-*/m2_inbound.py:30 | WANT = "refuse" | Decision 1 / H34, the value the product must have; seat.py:461 inlines it, no constant |
| 00109-*/acceptance.bash:132-135 | DOC_SECTIONS=(post-boot-health play-ledger play-freshness installed-vs-pinned) | The comment says they are spelled out so the check cannot agree with a producer that dropped one (status_document.py:72 owns BOOT_SCOPED_SECTION) |
| 00161-*/u20_check.py:111 | STOP_LINE "STOPPING BECAUSE: ..." | Required by the hooks daemon, not defined by the subject |
| 00203-*/hs_journal.py:39-75 | HS_RE, CATEGORY_KIND, windows | The analysis tool's own classification of journal lines |
| 00209-*/_acceptance-s1.inc.bash:450 | "limits.report_per_hour must be an integer from 1 to 60" | The plan requires "v1's words" (Go port parity); the bounds mirror helpers/pingbus/limits.py:46 |
| 00161-*/u01_probe.py:75-117 | CLAUDE_CODE_MESSAGING_SOCKET, `[uds-messaging]` regexes | Subject is Claude Code, external |
| 00104, 00143, 00149, 00185, 00198 probes | gsettings keys, login1, /etc paths | Probes of system state; no repo-owned value |

## UNSURE

| file:line | literal | note |
| --- | --- | --- |
| 00198-*/acceptance.bash:47-48 | `DBusActivatable=false`, `Exec=gnome-text-editor --new-window %U` | Equal to playbooks/imports/play-gsettings.yml:79-80, but the plan states it as the required outcome |
| 00144-*/acceptance.bash:93, 102 | extension file list, `container-watch.timer` | Product names, but stated as what must be deployed |
| 00109-*/acceptance.bash:116-139 | EXT_FILES, HEALTH_UNIT, COLLECT_TIMER, RECOVERY_* paths | Same; match files/ and play-displaylink.yml today |
| 00161-*/deploy_check.py:31 | REMEDY "choose a free address and pass it as --bus-address=<ip>" | Not located verbatim in agent-bus-install |
| 00161-*/_acceptance-u23.inc.bash:59; 00213-*/acceptance.bash:83-89 | /usr/local/share/agent-bus/kit paths, /opt/claude-yolo/optional/agent-bus | Copies of playbook destinations (play-agent-bus.yml:205, play-claude-yolo.yml:350) |
| 00201-*/_acceptance-m7.inc.bash:39-41, 188, 751 | /tmp/pingbus-home, settings.json, /run/ccy/membership | Copies of seat.py:71 SESSION_HOME, membership.py:36 CCY_MOUNT; comments cite seat.py. Weak INSTANCE |

## Counter-examples (the checker reads the product)

- 00161-*/u20_check.py:92-96 imports checkout, hooks, envelope, protocol; uses hooks.TEMPLATES[name], protocol.VERBS, protocol.PING_KEY.
- 00161-*/acceptance_check.py:90, 181 use forge.UNREACHABLE_REFUSAL, forge.RATE_REFUSAL, protocol.PROTOCOL_VERSION, cli.LINES.
- 00201-*/m7_check.py:150-175 takes ports from declare.choose_port.
- 00161-*/_acceptance-u20.inc.bash:249-250 and triage-ssh-agent-probe.bash:77 read CCY_VERSION and CONFIG_VERSION from the launcher.
- 00092-*/probe-host.bash:126 reads CCY_VERSION from the checkout and the deployed launcher.

## What text search found that reading could not have checked

Absence of a counterpart and exact subject locations: every row's "subject defines it at" came from a repo-wide grep of the literal, and
for A7-A11, A13, A14, B6, B10, B11 and B13 a hit or miss in the product is the only way to know the value is owned. A9 (v1.9.3 vs pin v1.9.4) and A1 are drift visible only by comparing with the subject.

## What reading found that text search could not have

- A3: the 3-space indent has no textual counterpart; it is display_width("👥 ") at statusline.py:352.
- A12: team_power_levels re-implements protocol.expected_power_levels; only the dict shape matches, no unique string.
- A16: NOTICE_RE reads FIRST_LINE for its head and retypes the tail; a search for FIRST_LINE shows a reader, not a copy.
- A17: the launch keys appear in the launcher as a `for key in` word list.
- B2, B3: layout and heading checks against Go printf and array code.
- The (c) calls on DOC_SECTIONS and WANT: a text match looks like a copy; the comments show it is deliberate.
- The counter-examples: telling checkers that already import the product from those that do not.

## Searches run

- Listing of check/probe/acceptance/triage files in every non-Completed plan folder.
- Module constants (`^[A-Z_]+ *=`), `readonly X=`, `grep -F/-q/-E` with a literal, `=~`, `== *"..."*`, `startswith(`.
- Per-literal grep of helpers/, files/, src-go/internal/, playbooks/, extensions/, run.bash for: STATUS_LINE_GUARD, "exec pingbus", DESCRIPTION_MAX,
  "several teams are active", "no verified team record", "This session is on", LAST_TEAMS, ccy-seats, PINGBUS_MEMBERSHIP, crossSessionInbound, keeper.lock, seat.lock,
  agent-bus-hs@, "removed by the owner", re-baseline, "relaunches once", PROBLEM, "the newest backup is", "is starting", agentbus0, tuwunel pin/version, FailedMark,
  FindingsChanged, org.fedoradesktop, "Insert does nothing while", "Dictation stats: ", SELF-UPDATE-CYCLE, "cannot read /workspace", "No rclone mounts found", "it can become",
  power levels, CCY_VERSION/CONFIG_VERSION, report_per_hour.
- Not examined in depth, so not claimed clean: deploy.bash files of 00058, 00158, 00160, 00165, 00173, 00195, 00196; 00170 unifi scripts; 00128 gate census; 00054 update.bash;
  the bodies of 00203 _acceptance-m4/m6.inc.bash beyond literal greps (M4_RECORDS/M6_RECORDS session-record path and `arg=` line format seen, not traced to the product);
  most of u01_probe.py (1107 lines) and triage_probe.py (1734 lines) beyond constants.
