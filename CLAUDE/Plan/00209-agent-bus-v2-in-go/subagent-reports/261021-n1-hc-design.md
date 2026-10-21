# N1 host-check design fixes (Plan 00209)

Branch `feat/bus-v2-n1-hc-design`, cut from 0ecc8409. Input: the "Findings on Tuwunel 1.9.4" of
`subagent-reports/261021-n1-hostchecks-handoff.md` on `feat/bus-v2-n1-hostchecks`, and the two open
minors of `261021-n1-decisions-check.md`.

## 1. HC-A1: a sync with `set_presence=offline` sets offline

**Fact.** On Tuwunel 1.9.4, `set_presence=offline` on a sync SETS the member offline; leaving the
parameter out marks it online. So no sync is presence-neutral. Default timers: `unavailable` after
300 s, `offline` after a further 1800 s.

**Fix.** Only the link syncs. Its long-poll, and a re-added team's first sync, are the only `/sync`
any member process makes. Every other read is a direct request: room state, `/messages`, an event
or account data. The rule "every other sync passes `set_presence=offline`" is removed everywhere it
was stated. C 3.2.4 sets `presence_idle_timeout_s = 120` and `presence_offline_timeout_s = 60`, so
a dead seat stops showing online within 2 minutes and shows offline within 3.

Why those values: a live link syncs at least every 30 s, and in a transient failure it waits at most
30 s between tries with a 45 s deadline. A healthy member therefore never drops to `unavailable`,
and a stopped team does, which is true. Shorter values would make a member flicker during a
restart's backoff. Longer ones would leave a dead seat looking alive for longer than a human waits.
A 14's Tuwunel-tagged presence row now asserts both the offline-setting fact and that a link syncing
every 30 s stays online past the idle timer.

**Sections.** A 6.6 (rule, display line, why-paragraph), A 12.1 agent row, A 14 server
behaviour row, A 16 HC-A1 row (recorded), A 17 OD-A1, A 18 presence risk, A 20 B and C
assumptions; B 2.6 step 09, B 2.11 presence paragraph; C 3.2.4 (new presence timers paragraph);
DESIGN-v2.md seams 11 and 22.

## 2. H-C13: a second login with the same `device_id` does not revoke the first token

**Fact.** Both tokens stay valid until a logout. One logout ends both, because they share the
device. No config key refuses a member's own password change; the only candidate is
`login_with_password`, which would also refuse the take. Logout, UIA on password change and
self-deactivation, `get_token` refused, the admin device and `deactivated` read, and the UIA
`DELETE /devices` all hold.

**Fix.** C 3.7.3 take step 3 is now: log in, `POST /logout` with the token that login answered,
then log in again and keep that token. The logout ends every token of the seat's device, so an
earlier token dies at the take whether or not its release ran. That covers a crashed session that
is over, a take that died between its login and `held/token`, and a copy in quarantine.

Three shapes were weighed:

- (i) Login, logout, login. **Chosen.** It needs no earlier token to be known, no UIA and no second
  device, and it is the same call as release step 4.
- (ii) Login, UIA `DELETE /devices`, login. It ends the same tokens. It also removes the device
  record, which buys nothing, and it costs a UIA round trip and a second send of the password.
- (iii) Logout with the previous `held/token` before the login. Not enough: that token is not
  always known, and where it is known the release has already logged it out.

Both "logout before login" options need an access token first, because every device call is
authenticated. That is why (i) and (ii) begin with a login.

Release step 4 now says that its logout ends every token of the device, and that a `held/` with no
`token` has nothing to log out. The old fallback paragraph is replaced by the weighed design. The
"config refuses password change if Tuwunel can" clauses become the recorded fact that it cannot.

K13 is restated: the release logs out, and the next take logs the device out before its own login.
A token whose release never ran therefore lives at most until the seat's next take or release. The
clause "open again only if H-C13 fails and both fallbacks fail" is removed.

DC16 is restated in both tables (C 4 and DESIGN-v2.md 7). C 3.14's seats test now requires
`fakehs` to model Tuwunel's rule: a second login keeps the device's earlier tokens, and one logout
ends them all.

DESIGN-v2.md 7.1, checked against the same fact. Under (d) no take logs in. Every pool token is
minted right after a password reset with device logout (the reset the restore's revocation also
rests on, H-C15). So the fact never applies, and take step 3's device logout is listed as dropped
under (d):

- (d1): the shared token is the device's only token.
- (d2): the release's logout ends it. A session whose release never ran is released, logout
  included, by the next take's step 1 before that take reissues.

The missing config key costs (d) nothing.

**Sections.** C 3.6.3 (the `password` credential), C 3.7.3 (take steps 1 and 3, release step 4, the
DC16 (c') and (d) bullets, the cost list, the new "How a take ends earlier tokens"), C 3.14 seats
row, C 4 DC16, C 5 K13, C 6.3 row, C 8 H-C13 (recorded); B 6 C10; DESIGN-v2.md 7 DC16 and 7.1.

## 3. H-C14: a `since` ahead of the database waits, then skips

**Fact.** It waits out the timeout and answers a `next_batch` at the server's lower position. A post
made after the restore is not delivered to a member holding the old `since`. A roster state read
answers at once.

**What was already said.** C47 states the requirement correctly. C 3.12.2 and B 2.6 step 08 already
drop the position on a changed `restore_epoch`, read by a direct state request, and resume from the
`teamchat.floor` marker and `released_ms` (step 01 (b)). Step 01 (a) already compares a store found
at start.

**The gap.** The read happened only "after every outage", relying on "a restore always stops the
homeserver, so every link sees an outage". A link can miss the outage:

- Go's transport silently retries a GET on a fresh connection when an idle kept-alive connection
  was closed.
- A team whose sync is paused on a full queue has no request in flight to fail.
- A stopped process has no request in flight either.

Such a link's next sync waits, gets the lower `next_batch`, persists it, and silently loses the
posts made after the restore.

**Fix.** B 2.6 step 08 now reads the roster's `restore_epoch` **before every long-poll**, and sends
the long-poll only when it is equal. A changed epoch re-verifies the room, drops the position and
resumes from the marker and `released_ms`. The cost is one small GET per long-poll, now in B 2.14's
cost table. B 2.15's replay row gains the case: a fake answering as Tuwunel does, with the restore
unseen by the link (an idle connection closed, a paused team), must deliver a post made after the
restore.

**Sections.** B 2.6 step 08, B 2.14, B 2.15, B 6 C13; A 5.1 `restore_epoch` row; C 3.12.2 (sync
paragraph), C 8 H-C14 (recorded); DESIGN-v2.md seam 58.

## 4. HC-A6: the service account is `@conduit`

Recorded in A 2.3 (reserved localpart `conduit`), A 16 HC-A6, DESIGN-v2.md seam 18 (now Met) and
PLAN.md Task 2.3.

## Also

- **Holding facts recorded.** HC-A4, HC-A7 and H-C12 hold on 1.9.4, and their rows say so.
- **A 14 Element row.** HC-A2's display half, HC-A3 and HC-A5 are the owner's steps, done by hand:
  first after gate A, against the first team on the host, then once per Element major change. They
  are not automated.
- **Decisions-check minor 1.** PLAN.md Task 2.5's gate B parenthetical now reads "H-C13 to H-C15,
  less H-C15's held-ccy-session clause, where they run on the host".
- **Decisions-check minor 2.** The C 3.7.3 DC16 (d) bullet, formerly line 1270, is rewrapped.

## Review round 1 (`261021-n1-hc-design-review.md`, FIX-FIRST, 2 major, 8 minor)

01. **Major: the epoch check guards the answer.** Reading the epoch only before the long-poll left
    a gap: a process frozen between the read and the send could miss a restore. B 2.6 step 08's
    epoch check now runs after every sync answer, before step 06 persists it, and only a checked
    answer is persisted. A read before the first long-poll after an interruption is kept; it only
    saves the 30 s wait. The check's own failures are handled: a transient failure persists
    nothing and sends nothing, a missing or undecodable roster is a trust failure, and a lower
    epoch counts as a change.
    Updated to match: B steps 01 (a) and 06, B 2.14, the B 2.15 replay row (a restore landing
    between the read and the send), B 6 C13, A 5.1, C 3.12.2, C 7 item 08, C 8 H-C14, seam 58, and
    PLAN.md Task 3.1's net/http resend sentence.
02. **Major: `--roster-from-backup` could repeat an epoch.** It now sets `restore_epoch` to a fresh
    random value in A 5.1's range. The value differs from the backup's and from the status
    record's copy of the roster. Members drop their position on any different epoch. Updated: C
    3.12.2's merged roster, C 3.6.1 and A 5.1.
03. The stale "after an outage" wording is fixed in C 7 item 08 and B step 01 (a).
04. **Presence timers.** C 3.2.4 now names the cases where a live member shows `unavailable`: a
    stopped team, a sync paused on a full queue (`unavailable` after 120 s, `offline` after 180 s),
    and a homeserver restart longer than about a minute.
05. **Login limits.** A take makes two password logins, so C 3.2.4 doubles v1's
    `[rate_limiting.login.account]` to a burst of 10 at 0.006/s. The failed-login limit keeps v1's
    values. Take step 3 says a 429 follows M6, and past its retries it is a take failure.
06. K13's bound is stated per case. A token whose release never ran lives until that release or
    the next take. A token minted by a take that died before `held/token` was never written and
    dies at the next take.
07. The "HC-A1 passed" wording now reads "held apart from `set_presence=offline`", in OD-A1 and the
    risk row. A 20 now says "roster only if OD-A1 (b) is chosen".
08. PLAN.md Task 3.1 is covered in item 1.
09. The epoch read's failure handling is covered in item 1.
10. Housekeeping:
    - C 3.2.1 now says "every member's link", not "keeper", holds the long-poll.
    - The touched long lines are rewrapped.
    - **For the integrator:** Task 1.4a is on `feat/bus-v2-n1-hostchecks`, not on this branch. It
      names these design fixes, so tick it together with this branch at the merge.

## Not changed

- No decision's state changed. The Decisions table and Task 0.5's lists are untouched: DC16 (c')
  still stands, with a different take step 3.
- HC-A2's server half is not recorded here: the hostchecks run did not judge it.
