FIX-FIRST

# Review of Plan 00209 Task 1.4 (host checks), `db3c8350..e747082f`

Adversarial review of `hca_server.py`, `hca_probe.py` and `test_hca.py`, the two reports and the
PLAN.md change, judged against A 16, A 6.6, A 14, A 4.2, A 6.10, C 8 (H-C12 to H-C14), C 3.7.3,
DC16, K13 and `restore_epoch` (A 5.1, B 2.6 step 08, C 3.12.2). The probe was not re-run.

The lead's decisions are carried out. There is no `hca_element.py`, `test_hca.py` does not import
it, and there is no host script and no meta-deploy entry. The three probes stay as Python in the
plan folder, and the server name is `hca.invalid`.

Mechanical results: `python3 -m unittest test_hca` (from the plan folder) runs 23 tests, OK.
`ruff check` passes on all three files (ruff 0.16.8), and `ruff format --check` reports all three
already formatted.

What holds up:

- The run report is faithful to the probe's own `report.md` in the untracked scratch run. The
  diff shows only heading levels demoted, the binary and scratch paths replaced by `<scratch>`,
  and "(not committed)" added. Every verdict in `261021-n1-hostchecks.md` matches the probe output.
- `hca_server.py` builds 4.2's power levels exactly (main/open questions, reports, closed
  questions) and 6.10's worker filter exactly. `allow_federation` is false.
- The claim that `restore_epoch` already covers H-C14 is right. C 3.12.2 says in so many words
  that "with the epoch read this way the design does not depend on that answer". B 2.6 step 01 (a)
  and step 08 read the roster by a direct state request before any sync from a stored position.
  The probe found a wait, an answer with a lower `next_batch`, and a fast direct roster read, which
  is exactly what that design needs.
- HC-A6 (`@conduit`) is correctly derived from the admin room's creator. The show-config dump also
  confirms it (`server_user_localpart = "conduit"`).
- Public-repo hygiene: no absolute host paths, user names, host names or container names appear in
  the committed files. The only absolute path, `/usr/local/lib/agent-bus`, is the repo's own
  install path. The worktree is named only by its repo-relative project name.

## Findings

1. **major — HC-A1: two presence facts have no baseline in the run, so the `PUT` offline PASS can
   be vacuous.** `hca_probe.py:605-613`: the `set_presence=offline` sync is judged by
   `GET /presence/status`, but on the main server the probe never reads that endpoint while the
   link is online. The "online" fact (lines 586-604) comes from the observer's sync presence
   events, a different source. `hca_probe.py:614-629` is worse. After the offline sync it re-syncs
   with the default and then `PUT`s offline, but it never checks that the re-sync made the link
   online again. If Tuwunel did not flip back (for example because of presence-update throttling),
   "PUT offline takes effect" reads `offline` and passes without the `PUT` having done anything.
   The FAIL of finding 1 is still credible: on the second, short-timer server the same `GET`
   endpoint does report `online at 0 s` (`presence_timeout`), and it agrees with how Tuwunel's sync
   pings presence. But the fact a design rule rests on should be self-contained. **Fix:** read
   `status_of(a)` and require `online` (record it as a fact) right after the first default sync and
   again right after the re-sync at line 614. Only then judge the offline sync and the `PUT`. Re-run
   it with the regression Go test, or in the probe the next time the probe is run. The committed
   verdict for the `PUT` fact should say that it was not baselined.

2. **major — H-C13: the fallback that passes is not the design's fallback, and the proposed fix
   ignores the design's own fallback paragraph.** C 3.7.3 (design lines 1298-1301) already says
   what happens when H-C13 fails: "a device id per claim (`TC<claim>`) and, at release,
   `DELETE /devices/<id>` with UIA by the pool's password, in place of the logout". At release the
   only token is the claim's own (`held/token`), so the fallback is **a device deleting itself**.
   `hca_probe.py:824-843` tests something else: the `HCASEAT` token deleting the different device
   `HCASEAT2`. "The fallback ... holds" (`261021-n1-hostchecks.md:62-64`, and the H-C13 row of the
   report table) is therefore claimed for an untested shape. Separately, finding 2 proposes a new
   "logout-before-login". That needs the previous holder's token. `held/token` survives a crashed
   claim, but nothing makes it available on every path, and the proposal does not cite or choose
   between this and the existing C 3.7.3 text. The run also shows that one logout ends every token
   of the device (`hca_probe.py:721-728`: after the second token's logout the first answers 401).
   That makes a third option available that needs no stored token: log in, log out, log in again.
   Its cost is a second login per take against the login rate limit (`rate_limiting.login.account`:
   a burst of 5, one more about every 5.5 min), and that is a real constraint to state.
   **Fix:**

   - (a) Add a fact that a device's own token deletes that device by UIA by password, and that the
     token is refused afterwards.
   - (b) Rewrite finding 2 to name C 3.7.3's existing fallback as the design path. List
     logout-before-login and login-logout-login as the options, each with its precondition (the old
     token held) or its cost (two logins per take against the rate limit).
   - (c) Say that K13 stays closed because a fallback holds. K13 reads "Open again only if H-C13
     fails and both fallbacks fail".
   - (d) Point at the step-3 sentence (design line 1182, "so the previous holder's token dies
     here"). It is now false on Tuwunel 1.9.4 and must change with DC16, and the C 15 seats test row
     (line 2306, "the previous holder's token is refused afterwards ... on Tuwunel by H-C13") must
     change with it.

3. **major — PLAN.md Task 1.4 is marked ✅, but nothing in PLAN.md tracks the design fixes it
   produced, and A 16 still says the opposite of the deferral.** `PLAN.md:131-138` states as current
   fact that "The Element checks (HC-A2's display, HC-A3, HC-A5) are owner steps in A 14's Element
   row, after gate A". A 14's Element row (design line 1937) does cover those behaviours, but A 16
   (line 1963) still says every check is "run before the build freezes the schema". HC-A3's "if it
   fails" is "7.5 and 10.4 are amended to the recorded shapes", and Task 1.3 (`proto`) is exactly
   what freezes those shapes. Moving HC-A3 to after gate A means `proto` is built on unverified
   Element shapes, and that consequence is recorded nowhere. The two failed checks also leave
   design text that is now known to be wrong, with no owner: A 6.6, A 15's B bullet (line 2062),
   B 2.6 step 09, A 14's server-behaviour row (line 1936), the A 16 HC-A1 row, C 3.7.3 step 3,
   DC16 and the C 15 seats row. **Fix:** keep Task 1.4 ✅ only if PLAN.md gains an open task (or
   sub-bullets) for those design-text changes, assigned to the design pass. That task should
   include a note in A 16 that HC-A2's display, HC-A3 and HC-A5 are deferred to A 14's Element row
   after gate A, and that 7.5/10.4 in `proto` stay provisional until HC-A3 is run. Otherwise mark
   Task 1.4 🔄 with those items open.

4. **major — finding 1's proposed fix contradicts B and is narrower than needed.**
   `261021-n1-hostchecks.md:55-57` proposes that "no request other than the link's long poll syncs
   at all". But B 2.6 step 01 (c) (a re-added team) syncs with a timeline limit of 0. That sync is
   also the link's and runs only while the seat is held. The fact that actually broke is "every
   other syncing request passes `set_presence=offline`". **Fix:** propose instead that every sync
   is the link's, runs only while the seat is held, and uses the default `set_presence`, with
   `set_presence=offline` never used. List every place the clause appears: A 6.6 (line 697), A 15's
   B bullet (line 2062), B 2.6 step 09, A 14's server-behaviour row (line 1936, "one with
   `set_presence=offline` leaves it unchanged") and the A 16 HC-A1 row. The report names only
   "A 6.6 (and B's matching step)", and A 14 only in passing (decision 2).

5. **minor — HC-A4 refusals have no positive control.** `hca_probe.py:313-317`: each refused type
   (reaction, sticker, both poll types) passes on any 403. A 403 for an unrelated reason (Tuwunel
   refusing the type or content outright) would read as 4.2 being enforced. **Fix:** send one of
   them as the admin (the creator, so everything is allowed) and require 200, so the 403s are shown
   to come from the power levels.

6. **minor — HC-A6: the localpart is a config key.** The show-config dump has
   `server_user_localpart = "conduit"`, so the reserved name depends on a default that could change.
   **Fix:** finding 4 should say so and propose that C 3.2.4 sets `server_user_localpart`
   explicitly (or that A's reserved list is derived from the config's value), so that a Tuwunel
   default change cannot move the service account onto a free handle.

7. **minor — H-C13's "no config key" fact rests on a substring scan.** `hca_probe.py:761-779`
   looks for keys containing `password` or `deactivat`. The dump is complete (348 rows), but a key
   that refuses these actions under another name would be missed. **Fix:** word the fact as "no key
   whose name mentions password or deactivation", which is what was measured. The conclusion (the
   health tick is the watch) is unchanged.

8. **minor — the handoff is stale against what landed.**
   `subagent-reports/261021-n1-hostchecks-handoff.md:29` and `:82-88` still say that `test_hca.py`
   imports `hca_element` and that the Element mode runs from `acceptance-hca.bash`. It is linked
   from the report as context. **Fix:** add one line at the top saying it is superseded by
   decisions 1 and 2 of `261021-n1-hostchecks.md` (no Element mode, no host script).

Counts: 0 blocker, 4 major, 4 minor.

## Round 2

MERGE

Re-review of the fix commit `a548c19a` (`e747082f..a548c19a`), read against the full task diff
`db3c8350..a548c19a` and against A 6.6, A 16, C 3.7.3 (the take steps, the fallback paragraph and
the health tick's foreign-device report), DC16 and C 15's seats row. The probe was not re-run.

Mechanical results: `python3 -m unittest test_hca` (from the plan folder) runs 23 tests, OK.
`ruff check` passes on all three files (ruff 0.16.8), and `ruff format --check` reports all three
already formatted.

The run report matches the probe code. Every new fact name in `261021-n1-hostchecks-run.md` is the
string the code builds: the two `online_fact` baselines ("after its default sync", "again after a
default sync, before the PUT"), the two admin positive controls, the renamed other-device delete,
`self_delete_fact`'s fallback fact, and the reworded config-key fact. The changed room and event
IDs, the `last_active_ago`, the device ID and the `since`/`next_batch` numbers are the new run's
own values, as a regenerated report should show. No absolute paths, user names or host names
appear in it or in `261021-n1-hostchecks.md`: the scratch and binary paths stay `<scratch>`, and
the only names are the probe's own (`hca.invalid`, `hca.N+lab.<kind>`).

### Round 1 findings

1. **FIXED.** `online_fact` reads `GET /presence/{a}/status` and requires `online` right after the
   first default sync and again right after the re-sync, before the `PUT`. Both hold in the run,
   so the `set_presence=offline` FAIL and the `PUT` PASS are each judged against a baseline of the
   same endpoint. The report says so in the HC-A1 row and in finding 1.
2. **FIXED.** (a) `self_delete_fact` deletes the device `HCACLAIM` with the claim's own token by
   UIA by password, and requires the token to answer `M_UNKNOWN_TOKEN` afterwards. That is C
   3.7.3's fallback shape (release has only `held/token`). It runs on the invitee account, so the
   seat account's burst of five logins is not exceeded: the seat takes four. The other-device
   delete is renamed for what it shows. (b) Finding 2 names C 3.7.3's per-claim device as the
   design path, and lists logout-before-login (needs the old token) and login-logout-login (two
   logins per take against the rate limit) with their costs. The statement that a crashed claim's
   device is cleared by the health tick's foreign-device report and the hot reissue matches C
   3.7.3's own text (design line 1285). (c) K13 is stated to stay closed, quoting K13's condition.
   (d) Step 3's "so the previous holder's token dies here" and C 15's seats row are named.
3. **FIXED.** PLAN.md gains Task 1.4a (⬜) for the design pass. It covers the presence clause in A
   6.6, A 14, A 15, A 16 and B 2.6 step 09, both timers and `server_user_localpart` in C 3.2.4,
   DC16's choice with C 3.7.3 step 3 and C 15's seats row, and the A 16 note that HC-A2's display,
   HC-A3 and HC-A5 are owner steps after gate A, with 7.5 and 10.4 in `proto` provisional until
   HC-A3 is run. Decision 1 of the report records the same consequence. Task 1.4 ✅ is right with
   that task open.
4. **FIXED.** Finding 1 now proposes that every sync is the link's, runs only while the seat is
   held, and uses the default `set_presence`, with `set_presence=offline` never used. It keeps B 2.6
   step 01 (c)'s timeline-limit-0 sync explicitly and lists all five places the clause appears.
5. **FIXED.** The admin (100) sends a reaction (the same content as the human's refused one) and an
   `m.poll.start`, and both answer 200. See new finding N1 on the poll's content.
6. **FIXED.** Finding 4 names `server_user_localpart` as Tuwunel's default and proposes that C 3.2.4
   sets it explicitly. Task 1.4a carries it.
7. **FIXED.** The fact's name and the report's wording are now "a config key whose name mentions
   password or deactivation", which is what the scan measures.
8. **FIXED.** The handoff opens with a line saying it is superseded by decisions 1-3 of the report.
   Decisions 1, 2 and 3 exist and cover the Element mode, the host script, the meta-deploy entry and
   the Python probes.

### New findings

N1. **minor — the admin's poll control does not send the content the human's poll was refused
with.** `hca_probe.py:318-320`: the human's `m.poll.start` is refused with
`{"m.text": [{"body": "p"}]}` (line 293), but the admin's control sends `{"body": "p"}`. The
reaction control reuses `refused[0][3]` exactly, so the reaction refusals are shown to be the
power levels'; the poll control proves the type is accepted, not that the refused content is. The
conclusion is unaffected in practice (Tuwunel does not validate poll content), but the fact name
claims more than the code shows. **Fix:** send `refused[2][3]` for the poll, as is done for the
reaction, when the probe is next touched or in the regression Go test.

N2. **nit — the report's markdown was not re-flowed after the edits.** In
`261021-n1-hostchecks.md` the HC-A4, HC-A1 and H-C13 rows of the checks table are longer than the
other rows (the table is no longer aligned), and decision 1's third line runs to 139 characters
while the rest wraps at 100. No QA gate checks this, and nothing reads differently. **Fix:** re-flow
on the next edit of the file.

Counts: 8 of 8 round-1 findings FIXED; new: 0 blocker, 0 major, 1 minor, 1 nit.
