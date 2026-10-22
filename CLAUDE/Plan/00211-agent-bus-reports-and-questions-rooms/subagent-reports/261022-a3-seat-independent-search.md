# Independent search: fixtures that build the subject's input by a route production never takes

Dispatch contained no rule or pattern (class description only). I did not open the detector
directories, tests/helpers/plan_acceptance/, src-go/qa/ or .semgrep/, and ran no detector.

Production reference (read, not assumed): helpers/pingbus/config.py load_bundle (249-274),
element_accepted (277-296), _check_linked_bundle (305-324). A `host` member loads beside an
Element profile only through a symlinked team entry (seat link, target real dir, owner uid,
mode 0700) plus the owner's accepted record. A plain directory is "a dedicated agent user's"
bundle and is refused beside Element. active_teams (142-159): with PINGBUS_MEMBERSHIP set, teams
come from the keeper's applied.json; PINGBUS_TEAMS is the fallback for "an agent-bus-claude
member, or a launcher before Plan 00203".

## Searches run
- Text over CLAUDE/Plan (not Completed; JOURNAL and subagent-reports ignored for verdicts),
  scripts/, tests/ (except plan_acceptance), helpers/, src-go (except qa), files/: `add-member`,
  `--out=`, `--type=`, `PINGBUS_HOME`, `PINGBUS_TEAMS`, `PINGBUS_MEMBERSHIP`, `member.json`,
  `seat take`, `seats/`, `ln -s`, `type host`, `member_type`, `user_home`, `Element`.
- Forms looked for: plain bundle dirs from add-member --out; hand-printed member.json/token;
  PINGBUS_HOME pointed at a non-seat dir; PINGBUS_TEAMS instead of PINGBUS_MEMBERSHIP; hand-made
  seat dirs and session homes; stubs standing in for the real loader.
- Then read each hit's code and the production code it stands for.

## Instances

1. INSTANCE (confident). CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash
   lines 353-364 (a3_take) and 396-403 (a3_check). Found by: the dispatch plus text search
   (`add-member`, `--out=`); confirmed by reading.
   Fixture: `agent-bus add-member ... --type=host --out=<run>/a3-pingbus/<team>` (plain dir),
   PINGBUS_HOME=<run>/a3-pingbus, run by the desktop user under `env -i`. Production: seat take
   places the bundle in the checkout seat dir, the session home links to it. Result: refused
   beside the owner's Element profile. Also differs: env has HOME and PINGBUS_TEAMS only, no
   PINGBUS_MEMBERSHIP, no notify socket.

2. NOT-AN-INSTANCE (reasoned). CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/_acceptance-m7.inc.bash:367-374
   (m7_add_member: `--type=host --out=${M7_MEMBERS}/<team>/<repo>`, token removed). Found by text
   search (`--type=`). Same fixture shape as A3, but pingbus never loads it: the bundle is read
   only for base_url/room (line 418) by the human-login topic check. The pingbus calls (587) run
   in the real session container whose seats come from ccy's seat take. Would become an instance
   if anything later ran pingbus on those dirs.

3. NOT-AN-INSTANCE (reasoned; differs from the desktop owner case). CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-steps.inc.bash:168-201
   and member_run 68-86 (add_member `--type=host --out`, PINGBUS_HOME=/var/lib/<unit>,
   DynamicUser). Found by text search. A plain dir for a dedicated agent user with no Element
   profile is the documented production route (README.host; config.py "a dedicated agent user's").
   Small doubt: the account is a transient DynamicUser, not a persistent agent user.

4. NOT-AN-INSTANCE. CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u20.inc.bash (seat take at
   launch; host-side status home at 525-543 links PINGBUS_HOME to a real seat, as production does),
   _acceptance-u23.inc.bash:429-435 (docker/vm via the kit README steps),
   CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/_acceptance-goodbye.inc.bash:311 and
   CLAUDE/Plan/00203-teams-hot-control-and-robustness/_acceptance-m6.inc.bash:270 (real
   `agent-bus seat take`). Found by text search (`seat take`, `PINGBUS_HOME`); confirmed by reading.

5. UNSURE (low). Plan 00211 session_run, lines 86-93, used by the test question
   (ask_test_question 516-534), A5 and A4's ccy half: `podman exec -i -e
   PINGBUS_HOME=/tmp/pingbus-home -e PINGBUS_TEAMS=<team> <ctr> pingbus ...`. The seats are
   production-built (seat take plus seat exec link), but the environment is built by hand:
   PINGBUS_TEAMS (the pre-00203 fallback) instead of the PINGBUS_MEMBERSHIP that seat exec gives
   claude; no notify socket/token env; the default exec user is not shown to equal the user claude
   runs as. Same shape in 00201 m7_bus_exec (587), 00203 m3 (232, 266) and m4 (306); the m7 comment
   itself says a bare `podman exec` lacks what seat exec gave claude. Found by text search
   (`PINGBUS_HOME`) and reading config.active_teams. It exercises a supported config path, not the
   one a live session takes.

6. UNSURE (low). Plan 00211 in_guest (474-480) for A4 and LXC session reads: `env -i ...
   HOME=<home> PINGBUS_TEAMS=<team>`, no PINGBUS_HOME (default ~/.config/pingbus, where the play
   put the bundle; type lxc, so no Element rule). The production env file also sets
   HOOKS_DAEMON_HOSTNAME (helpers/agent_bus_lxc/members.py lines 21, 1034); I did not trace
   whether `pingbus report` needs it. Found by reading members.py against the fixture.

7. NOT-AN-INSTANCE with a note. CLAUDE/Plan/00209-agent-bus-v2-in-go/_acceptance-s1.inc.bash:381-440
   (fixture_member prints member.json by hand, `.host` handles, mkdir seat dirs with default
   modes, no token, hand-made session record). Found by text search (`member.json`). It tests
   only the listing reader (`ccy-teams --list`), not load_bundle, and runs no pingbus as a host
   member. Production seats are 0700 with a token; the Go reader checks only symlink/regular
   file (bus/seat/seat.go:155, member.go:125). Would be an instance if judged by anything that
   checks mode or token.

8. NOT-AN-INSTANCE. scripts/test-ccy-agent-bus.bash:318-326 (write_seat) and
   scripts/test-ccy-teams-command.bash:568-571 (seat_add): hand-printed podman-type bundles (the
   second "as a seat take leaves it") fed to the real zipapp cli.main / seat listing. Type is
   podman, so no Element rule applies. Found by text search.

9. NOT-AN-INSTANCE. tests/helpers/pingbus/test_config.py (BundleCase; TestElementProfile 606;
   TestElementAcceptedForADesktopSeat 651) builds bundles in temp dirs with user_home injected and
   covers both the plain and linked routes on purpose: it is the unit under test.
   tests/helpers/agent_bus/test_checkout.py:389-443 uses fake wrappers for take_actions.
   tests/helpers/agent_bus_lxc/test_members.py refuses `--type=host` on purpose.

No other add-member / PINGBUS_HOME / seat-dir fixture exists in active plans, scripts/ or tests/
(00213, 00215, 00196 hits are deploy or prose only; 00213's acceptance unsets PINGBUS_*).
Not read: Go test fixtures under src-go/internal (seat/, teams/, statusline/) that build bundles
in Go. They matched the grep but I did not read them individually, so they are UNREAD, not cleared.

## Plan 00211 A4 and the test question (never run on a host)
- A4 (a4_check, 499-508): does not use the A3 route. It runs the real `pingbus report` in an LXC
  container through in_guest, on a member whose bundle the play wrote (production route), type
  lxc, so the Element rule does not apply. Differences are env only (item 6).
- Test question (ask_test_question, 516-534) and A5: the coordinator's own live ccy session, seats
  from seat take, home /tmp/pingbus-home from seat exec; the fixture is the live session, not a
  hand-built bundle. Env differs (item 5). If the coordinator is an LXC member, same as A4. If it
  is a host member it is not driven (find_session says so, NOT JUDGED), so no plain-bundle route
  arises.
- Neither builds a bundle by hand. The remaining doubts are items 5 and 6, which only a host run
  can settle.

## Attribution of findings
Text search found the candidates 1-4, 7, 8 and ruled out 00213/00215/00196 by absence. It could
not tell INSTANCE from NOT-AN-INSTANCE; every verdict rests on reading. Reading alone found:
that item 2's bundles are never loaded by pingbus; that item 5's PINGBUS_TEAMS vs
PINGBUS_MEMBERSHIP difference exists (grep matches both routes alike; only config.active_teams
shows they differ); item 6's missing HOOKS_DAEMON_HOSTNAME; item 7's missing 0700/token; and
the production rule itself.
