# Review: `fix/teams-signin-first-run` (14b211ad) against F44

Adversarial pre-merge review of the three ccy-teams first-morning fixes. Read-only; no qa-all
run. Method: read the full diff, read the code it lands in (`login.go`, `board/snapshot.go`,
`board/collect.go`, `picker/keys.go`, `flow/kit.go`, `ui/picker.go`), ran the Go tests of the
three touched packages on the branch in a scratch worktree (green), then overwrote `login.go`
and `offers.go` with their F44 versions in that worktree and re-ran (red), then removed the
worktree.

## Verdict: MERGE

No blocking finding. Two advisory notes, one of them on the ACCEPTANCE step 1 wording the
coordinator asked about.

## Fix 1: no "Copy it now" after a first password

Correct. `OpenElement` now says "Press Enter to open Element, then sign in there as NAME with
the password shown above." That sentence is true on every branch of `offerCopy`: Clipboard
Indicator on (not offered, typed from the screen), off and copied (also still on screen), off
and declined, no gsettings (offered and copied), and outside Wayland. `showLogin` always prints
the password to the terminal before any of these, so "shown above" holds.

Tested: `TestElementAfterAFirstPasswordNeverSaysCopyIt` runs four of those cases and asserts
both the absence of "Copy it" and the presence of the new sentence; the pre-existing
`TestElementOffersNeverSignedInPeopleANewPassword` is pinned to the new text. With `login.go`
reverted to F44 both fail. `TestElementNeverSignedInDeclinedStillOpens` was correctly re-pinned
to the new phrase so it still proves nothing is said when no password was set.

## Fix 2: "New Element password?" defaults to Yes only for a read count of 0

Correct, and it cannot default to Yes when a reset would sign out a session:

- `SetPassword` uses `count, known := snap.SessionsOf(team, name)` and passes
  `known && count == 0`. `SessionsOf` is a plain map lookup on `Snapshot.Sessions`.
- `Snapshot.Sessions` is written only by `setSessions`, which is called only from
  `collectMembers` on a successful `HumanDevices` read (or from the collector's cache of such a
  read). A `sudo.ErrNeedsPassword` or any other read error sets `SignedIn` to a word and never
  touches `Sessions`, so `known` is false and the default is No. A roster read failure returns
  before the loop, so again nothing is in `Sessions`.
- Unknown count: default No (`known` false). Any positive count: default No. So the only path
  to Yes is a successful read that returned zero devices, which is exactly
  `WordNeverSignedIn` (`signedInWord(0)`).
- No other caller is affected: `passwordFor` (the P key) already bypasses `SetPassword` for
  `known && count == 0` and calls `IssuePassword` directly, so for that path `SetPassword` is
  only ever reached with the default evaluating to false, as before. `askSignIn` (post-apply
  sign-in gap) uses its own `Gum.Confirm` with `true` and is unchanged.

Tested: `TestElementFirstSignInDefaultsToSettingAPassword` asserts `--bind=start:last` is absent
on the fzf call (the Confirm picker adds it to put the cursor on No). Red with F44 `login.go`.
The No defaults for a positive count and for a bypassed P key remain pinned by
`TestPasswordForSomeoneSignedInNamesTheSessionsItEndsAndDefaultsToNo` and
`TestPasswordRowSetsAPasswordAndShowsTheLoginOnce`.

Advisory A (non-blocking): `TestPasswordWhenTheCountIsUnknownSaysTheSessionsAreSignedOut`
exercises the unknown-count branch of the sentence but does not assert the default (no
`start:last` check). The `known &&` guard makes a Yes default structurally impossible there,
but a one-line assertion in that test would pin the invariant the coordinator asked about
rather than leaving it to code reading. Worth adding in a follow-up, not a reason to hold the
merge.

Stale board: the collector caches device counts per person until `ForgetDevices(team)`, which
runs after every password issue and every Element launch from ccy-teams. A count of 0 can be
stale only if the person signed in by a route ccy-teams did not start (another machine, a
phone) while the picker stayed open. That window is pre-existing: the same cached 0 already
gates whether the offer appears at all (`WordNeverSignedIn`) and already makes the P key skip
the question entirely in `passwordFor`. The branch does not widen it.

## Fix 3: "Create a team ... now?" defaults to No

Correct and minimal: `offerCreateHere` passes `false`. Tested by
`TestOfferJoinHereWithNoTeamForTheRepositoryOffersToCreateOne` (gum log pinned to
`--default=false`; red with F44 `offers.go`) and the bash gate pin in
`scripts/test-ccy-teams-command.bash` changed to match.

## No other default changed

Every other `AskYes`/`Confirm` default in `picker/offers.go` (create-one-now on an empty
machine: No, already; remove-limit: Yes; write-description: Yes; update-now: Yes; both
start-a-session questions: Yes) and in `login.go` (`offerElement`: Yes; `setUpElement`: Yes;
`offerCopy`: Yes; `askSignIn`: Yes) is untouched by the diff.

## SPEC-many-teams.md sentence

Consistent. The S-B15 addition says the create-team question defaults to No and both
start-a-session questions default to Yes, which matches `offers.go` lines 292 and 309 and the
ACCEPTANCE step 2 wording on the branch ("start a session there (default Yes), or to create a
team for it (default No)").

## "No version bump needed" is right

The CCY bump rule (`.claude/rules/ccy-version-bump.md`, `CLAUDE/ContainerRules.md` "What
requires a version bump") covers `files/var/local/claude-yolo/claude-yolo`, every
`lib/*.bash`, the one-key agent in `helpers/ssh_agent_filter/`, the `Dockerfile` and
`entrypoint.sh`. The branch touches none of them: ccy-teams is a Go binary built by
`play-claude-yolo.yml` ("Build and Install the ccy-teams Command"). No image or launcher change,
so no ccy or container version bump, and no session relaunch.

## meta-deploy.list entry and ACCEPTANCE step 1

The list entry is well formed (one comment block describing the run, then
`playbooks/imports/play-claude-yolo.yml`, the second kind the header allows) and names the
right play: that play is what rebuilds ccy-teams.

Advisory B (non-blocking, but decide before the morning): ACCEPTANCE step 1 now says "The
queue holds one run ... run `./CLAUDE/Plan/meta-deploy.bash`". If the coordinator has the
host run meta-deploy tonight, by the morning the queue is empty (the standing rule says the
entry is removed once run) and step 1 is wrong on two counts: it tells the owner to run a
script that will print "The queue is empty. Nothing to run.", and "already runs F44 at
df548372" is stale (the host will be at the merge of 14b211ad or later). Running it is
harmless (it does nothing), so this is wording, not safety. The right move is: merge as is,
and when tonight's host run passes and the entry is removed from meta-deploy.list in that
same commit, restore step 1 to its "Nothing to deploy" form with the new commit and the same
"no ccy or image change" note. If the host run is NOT done tonight, step 1 is correct as
written and should stay.

## Public-repo hygiene

Clean. The diff uses only fixture names (`owner`, `alpha`, `example-org/proj`,
`wayland-test`) and the permitted self-reference `fedora-desktop`. No hostnames, usernames,
container names, paths outside the repo, or credentials. The JOURNAL and PLAN entries name
only branch names, test names and commit hashes.

## Test evidence

- Branch, scratch worktree: `go test ./internal/teams/login/ ./internal/teams/picker/ ./internal/teams/board/` all ok.
- Same worktree with `login.go` and `offers.go` at F44 and the branch tests kept:
  `TestElementOffersNeverSignedInPeopleANewPassword`,
  `TestElementFirstSignInDefaultsToSettingAPassword`,
  `TestElementAfterAFirstPasswordNeverSaysCopyIt` and
  `TestOfferJoinHereWithNoTeamForTheRepositoryOffersToCreateOne` fail; nothing else does.
- The bash gate `scripts/test-ccy-teams-command.bash` was not run here (it needs `script`
  and a full build; it is in `qa-gates.tsv` and will run in the coordinator's qa-all).
