FIX-FIRST

# N2 slice n2c review, round 1 (seat store, `feat/bus-v2-n2a..feat/bus-v2-n2c`)

## What was run, and what it showed

From `src-go/` in the slice's worktree:

- `go test -race -cover ./internal/teamchat/seat ./internal/teamchat/paths`: ok. seat 90.2%,
  paths 100.0%.
- `golangci-lint run ./internal/teamchat/...`: 0 issues.
- `golangci-lint run --build-tags tuwunel ./internal/teamchat/seat/...`: 0 issues.
- `FAKEHS_TUWUNEL_BIN=<pinned tuwunel-1.9.4> go test -race -count=1 -tags tuwunel -run TestTuwunel ./internal/teamchat/seat`:
  `TestTuwunelTakeAndRelease` PASS. The stale TCSEAT token dies at the take, and the take's
  token dies at the release.
- Two reproductions, kept untracked under `untracked/scratch/review-n2c/` and compiled into the
  package with `go test -overlay` so that no tracked file changed. Each is described with its
  finding: `race_review_test.go` (findings 1 and 2) and `case_review_test.go` (finding 4,
  formatting and marshalling of a secret).

Gate B holds. The diff is inert Go and tests: there is no play, unit, sudoers, systemd or godbus.
The one exception is finding 9, a fixed test password.

Secret handling was checked. `%v`, `%+v`, `%#v` and `%s` of an `Issued` all print `[secret]`.
`json.Marshal(Issued)` is refused. No error path prints the password: the existing tests cover
`Mint`, and I read every `fmt.Errorf` in the package.

## Findings

01. **High: a release retires the next holder's token, and writes its notice into the next
    holder's `held/`.** In `release.go:67-94`, `release` reads `held.json` once, at line 69, outside
    the allocation lock. It then runs step 3 (`announce`, which writes `pool.HeldNotice()`), step 4
    (`retire`, which reads `pool.HeldToken()`) and step 5 (`finish`). None of these steps checks
    again that `held/` still carries `rec.Claim`. This can happen while a release of an ended
    session is outside the lock (a slow announcer or logout). A take for another session can then
    meet the same stale `held/` and release it itself under its own lock (n2c-D4, `take.go:182`).
    It then makes a new `held/` and mints a token. When the first release resumes, it records its
    notice (with the old claim) into the new `held/notice.json`. It then logs out the new holder's
    `held/token`. Last, it fails at step 5 with `EEXIST`, because the quarantine name for the old
    claim already exists. The reproduction (`TestReviewReleaseRetiresTheNextHoldersToken`) shows
    this: s1's release fails with "file exists", and s2's token is dead
    (`alive(token2) == false`). s2's `held/` and session seat are still in place, and s2's
    `held/notice.json` carries s1's claim. The new session holds a seat with a dead token until
    someone reissues it. **Fix:** before each mutating step (3, 4 and 5), re-read `held.json` and
    stop if its claim is not `rec.Claim`: the release is done, or ErrHeld, since another holder now
    has the seat. Step 5 does this under the lock it already takes. For steps 3 and 4, read
    `held.json` and `held/token` together and compare claims before the announcer or `Retire` is
    called. The notice write should also refuse a `held/` whose claim has changed. Add a test that
    interleaves a take inside the announcer, as the reproduction does.

02. **High: a release or `Announce` by any session can quarantine a live take's complete
    holding.** In `release.go:120-133`, `heldRecord` decides that `held/` is incomplete without the
    lock: it found `held/` but no readable `held.json`. It then calls `locked(...)` and quarantines
    `held/` without looking again. A take holds the lock between `mkdirat(held)` and the
    `held.json` create (`take.go:202-210`). A `Release` or `Announce` that probes during that
    window decides "incomplete" and blocks on the lock. By the time it gets the lock, the take has
    finished. It then moves the complete `held/`, live token included, to quarantine. It does not
    unlink the token (`unlinkName ""`) and never retires it. This happens before any check that
    the session matches (`rec.Session != r.Session` is checked only after `heldRecord` returns), so
    a release for an unrelated session does it. The reproduction (`TestReviewReleaseQuarantinesALiveTake`)
    shows this. It hides `held.json` while the test holds the lock, starts `Release(s9)`, restores
    `held.json`, then unlocks. The result is `release: <nil>`, `held/ present: false`, s1's session
    seat present, and s1's token alive. The pool now looks free, so the next take mints again, and
    two sessions are on one seat until the device logout kills s1. **Fix:** inside the locked
    function, re-read `held.json`. Quarantine only if it is still missing or broken; otherwise use
    the record. When quarantining, also unlink `token`; the design says the next take's logout ends
    it, but no live token should be left lying in quarantine. Add a test with a gated clock, as the
    reproduction does.

03. **Medium: a placement over an existing record drops `released.json`, so the next take writes
    `released_ms: null`.** `place.go:109-119` (`swapIn`) uses `RENAME_EXCHANGE` to move the old
    pool directory, `released.json` included, to quarantine. The new claim holds only the
    credential and `seat.json`. After a `return` or `reissue`, the next take's held `seat.json`
    says the seat was never released (`take.go:217-223`). C 3.7.2 says `released_ms` bounds area B's
    store-less start because "a marker alone is not a fact the host wrote" (security 1, seam row
    32). A null value leaves B with only the marker, which a token set. SP2 is silent on this, and
    the code follows SP2. It is a gap in the design that the slice should decide, not inherit.
    **Fix:** in `Fill`, before the swap, copy the old record's `released.json` (SW3 read, SW1
    create) into the claim, and add a test that a take after a reissue carries the earlier
    `released_ms`. Alternatively, record the decision as n2c-D8 and propose the design-text fix.

04. **Low: strict decoding accepts repeated keys that differ only in case.** In `records.go:154-179`,
    `decodeStrict` checks repeats and the required keys with `proto.Parse`, which compares keys
    exactly. It then decodes with `encoding/json`, which matches field names case-insensitively,
    so `DisallowUnknownFields` lets `"ISSUED_MS"` and `"Issued_Ms"` through, and the last one wins.
    The reproduction (`TestReviewCaseFoldedKeys`) shows a pool `seat.json` carrying `issued_ms`,
    `ISSUED_MS` and `Issued_Ms` decoding with no error. This breaks SR1 and SR2 ("no unknown or
    repeated key"). It also applies to the seat lock record that `runOf` decodes, and a container
    writes that one. **Fix:** after `proto.Parse`, require that the object's member names are
    exactly the `want` set (`len(v.Members()) == len(want)`, each name in `want`), and add the
    case-variant cases to `TestBundleRefusals` and `TestRecordsRefuse`.

05. **Low: a retired seat held by a live session leaves a live token in quarantine, and that
    session's release does nothing.** `take.go:101-105`: `checkState` quarantines the whole pool
    directory before `clearHeld` runs, and unlinks only the credential. A `held/` naming a live
    session goes along with it, `held/token` included. That session's later `Release` finds no
    `held/` and returns nil, so no logout happens. **Fix:** for `StateRetired`, check `held/`
    first. Either refuse with ErrHeld when it names a live or restorable session, or release a
    stale one first. Also unlink `held/token` when quarantining. Add a test.

06. **Low: a take checks the session seat directory, then writes into it by path, not through
    the fd it checked.** In `take.go:277-291`, `copyToSession` runs `checkEmptyOwned` and then two
    `create` calls, each of which re-resolves the path. ST5 and C 3.7.3 steps 6 and 7 write
    relative to the fd that was checked. `RESOLVE_NO_SYMLINKS` still refuses a link, and the
    design's argument ("a directory of its own ... only receives the copy") limits what this
    costs. Even so, the owner and mode check no longer covers the directory that actually receives
    the token. **Fix:** keep the fd from the check and `createAt` both files on it, or say in the
    SPEC why checking and then writing by path is equivalent.

07. **Low: `mkdirAll` accepts an existing directory of any mode or owner, and makes no `fsync`.**
    In `fs.go:215-242`, `mkdirAll` opens each existing directory but never checks it with
    `ownedDir`. Both `KeepGuestToken`'s `guest/` (SP4: "its directory made 0700") and
    `sessions/<session>/` pass through it. A `guest/` already at 0755 stays 0755, which breaks
    SP4. A new directory is made without `fsync`ing its parent, unlike `mkdirFresh` (SW1).
    **Fix:** after the `Openat2`, run `ownedDir` on the fd, and `Fsync(parent)` after a successful
    `Mkdirat`. Add a test for `KeepGuestToken` over a 0755 `guest/`.

08. **Low: some names are spelled out instead of coming from `teamchat/paths` (SN1).** Examples:
    `release.go:272` (`"token"`), `place.go:91` (`"/seat.json"`), `guest.go:184,187`, and
    `take.go:116,263` (`pool.Dir()+"/"+credentialName()`, although `PoolSeat.Password()` and
    `Token()` exist). **Fix:** use `pool.HeldToken()`, the `Seat`/`PoolSeat` accessors, and a
    `paths` function from `Kind` to file name.

09. **Low: the test seat password is a fixed literal.** `world_test.go:20`: `testPassword = "pool-password-1"`
    is used on fakehs and as the seat account's password on the real Tuwunel
    (`tuwunel_test.go:52,109`). Gate B, as SPEC-n2c.md words it, is "The only passwords are
    random test keys", and the admin password in the same test already uses
    `tuwunelhs.RandomKey(t)`. **Fix:** generate the password once per world (`tuwunelhs.RandomKey`,
    or `crypto/rand` hex in the untagged world), or change the gate's wording to allow a fixed
    password on a throwaway server.

10. **Low: `closeQuietly` hides a discarded error.** `fs.go:115-119`: `if err := unix.Close(fd); err != nil { return }`
    discards the error just as `_ =` would, which goes around the "no discarded errors" rule
    while satisfying the linter. **Fix:** return the close error through `errors.Join` at each
    caller (`inParent`, `mkdirAll`). If a read-only directory fd's close can never matter, say so
    in GoProjectRules.md as the project's one sanctioned form, rather than leaving a no-op branch.

11. **Low: the credential rule is stricter than the SPEC says.** `records.go:260-276`: `ParseSecret`
    refuses a space. SR3 and C 3.7.2 say "printable ASCII, no newline", which allows one. A
    password with a space, from the root side or a reissue, would be refused at `Fill` and at
    `Mint`. **Fix:** either allow 0x20 (and keep refusing control characters), or make SR3 say
    "without a space" and record it as a decision.

## The builder's decisions (handoff and since)

- The probe opens from the root's fd with `RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS`, not from
  `sessions/<session>/`. Sound: it refuses a link on every component, as the design asks. Propose
  the change to the design text, as the handoff says.
- A free lock over 4 KiB names no run. Sound, and consistent with SD1.
- Step 1 stops waiting when the lock is free or the session is not restorable. Matches SX1.
- A `held/` with no complete `held.json` is quarantined by a release as well as by a take.
  Sound as an idea, but it is the source of finding 2: the decision must be made under the lock.
- An `Outcome` neither posted nor carrying a one-line `why` is refused. Sound. Note that this
  stops the release on every retry until the announcer is fixed, which is the right failure.
- Step 4 retires with the pool's `seat.json`. Sound.
- `PlaceGuest` removes `<team>.new-*` leftovers first. Sound for a guest, which has no quarantine.
  The team grammar has no `.`, so the prefix cannot match another team. There is no lock, so two
  concurrent guest placements could remove each other's claims; the caller (`lxc apply`) must
  serialise them. Note this in the doc comment.
- `rel` answers `"."` for the root itself. Correct. `entries(root.path)` and `mkdirAll` depend
  on it.

## Coverage of the numbered expectations

Every SN, SR, SW, SL, SC, ST, SX, SD and SP item has a test that drives its path. ST7 (ccy and
host `cc` take the same way) holds by construction, since there is one `Take`, and needs no
separate test. Missing tests correspond to the findings: none interleaves a release with a take
(1, 2), and none covers a reissue's `released_ms` (3), case-variant keys (4), a retired seat that
is held (5) or a non-0700 `guest/` (7).
