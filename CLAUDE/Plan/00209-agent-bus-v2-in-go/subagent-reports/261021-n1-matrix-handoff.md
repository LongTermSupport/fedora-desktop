# Plan 00209 N1 Task 1.5 (matrix): handoff

Worktree `/workspace/untracked/worktrees/bus-v2-n1-matrix`, branch `feat/bus-v2-n1-matrix`, tip
`c78912b4`. Read `/workspace/untracked/scratch/n1-common.txt` (the common rules) and the lead's
Task 1.5 brief first. All work is committed; the tree is clean apart from untracked scratch.

## Done

- The rebuild: M1's 8 commits (31ae6433..d7a439de) on db3c8350, byte-identical to the reviewed
  58c7b6e1 (`git diff feat/bus-v2-m1-matrix d7a439de` over the package, the SPEC and
  `.golangci.yml` is empty). `git merge-base --is-ancestor 35acceab HEAD` exits 1.
- `5d5bb7be`: the move to `src-go/internal/teamchat/matrix` (and `fakehs`), imports, golangci path
  rules, SPEC renamed; the package doc says why net/http's resend of a dropped `/sync` is safe
  (a sync with a since token is idempotent), and so does SPEC section 7a's last paragraph.
- `3c34cde6`: SPEC M45-M48. `LoginDevice` (fixed device ID, C 3.7.3 take step 3), `Logout` (release
  step 4; `M_UNKNOWN_TOKEN` left to the caller), `RoomAccountData`/`PutRoomAccountData` (the
  `teamchat.floor` marker, B 2.3.2), in `seat.go`; fakehs models them (`fakehs/session.go`); the
  tagged real-Tuwunel test has `seatCycle` (runs only on the host).
- `ffbb65a0`, `0052169b`: SPEC M49-M51. `Redact` (C DC20), `SyncRequest.SetPresence` and
  `SetPresence` (A 6.6, B 2.6 step 09); fakehs keeps the redaction reason, marks the redacted event
  with `unsigned.redacted_because` (A 7.6 rule 02), keeps presence (`Server.Presence`).
- `41c72fe4`: review round 1, FIX-FIRST (2 major, 4 minor),
  `subagent-reports/261021-n1-matrix-review.md`.
- `c78912b4`: findings 1 (v1 names; the replay keeps three deliberate references, owed
  `allowed.tsv` lines in Task 1.2), 2 (`Redact` makes one try; fakehs txn per endpoint, replayed
  redact unmodelled) and 5 (fakehs sync `set_presence` per the spec; receiving presence owed to N3).
- Coverage: matrix 98.8% (floor 98.7%), fakehs 92.7%. Lint clean with and without `-tags tuwunel`.
  qa-changed is red only on GO-2026-6809 (Go 1.26.9; Task 1.6, the coordinator's).

## Left

1. Review finding 3 (minor): strengthen `seatCycle` in `tuwunel_test.go`: check `Error.Errcode == "M_UNKNOWN_TOKEN"` for the replaced and the logged-out token; send a second `Logout` with the
   dead token; decode the marker and compare `floor_ms` 1; add `admin.ResetPassword` between the
   release and the last take (log in with the new password); a wrong-password `LoginDevice` is
   `ErrForbidden`. Vet and lint with `-tags tuwunel` (it cannot run in the container).
2. Finding 4 (minor): `RoomAccountData` (`seat.go`) gives `found=false` only for a 404 whose
   `Errcode` is `M_NOT_FOUND`; any other 404 is the error. Test first: inject a 404
   `{"errcode":"M_UNRECOGNIZED"}` in `TestRoomAccountDataRefusals` (`seat_test.go`). Update M47.
3. Finding 6 (minor): the package docs cite M1-M36 (`client.go:1-3`) and M37-M43 (`fakehs.go:2`);
   extend to M45-M51 and M48/M51.
4. Then review round 2 (opus, same brief, scope d7a439de..HEAD, writes
   `subagent-reports/261021-n1-matrix-review-2.md`), fix, commit each review file; at most 3 rounds.
5. The report `subagent-reports/261021-n1-matrix.md`: what was built, coverage, the decisions
   below, the design-text fixes, and the gaps for N2/N3 below.

## Decisions taken (for the report)

- One file per concern: `seat.go` (LoginDevice, Logout, room account data); `Redact` beside
  `Send` in `member.go` (shared `transactionID`); presence in `sync.go`.
- `LoginDevice` sends no display name and no refresh token, and needs the answer to name the
  device asked for; a device ID is 1-255 bytes of printable ASCII without a space.
- `Redact` does not retry a lost answer (finding 2); `Send` still does (recorded for `/send`).
- fakehs follows the Matrix specification where H4 recorded nothing and the design needs it
  (device login, logout, account data, presence, `redacted_because`), and leaves unmodelled what
  neither says: another account's account data or presence, an unknown room, non-object content,
  a replayed redact transaction ID, a presence value beyond the three.
- User-Agents are `teamchat` and `teamchat-admin` (v1 names are out of `teamchat/`, D 4.1).

## Gaps for N2/N3 (not built)

- H-C13's negative claims (a token cannot change the password, add a device, or mint a login
  token without the password) need raw calls the client does not offer; N2's host check.
- The H-C13 fallback (`DELETE /devices/<id>` with UIA) only if H-C13 fails.
- Receiving presence for `status` (A 6.6), with HC-A1: N3.
- Whether Tuwunel stores a replayed redact once: unknown; nothing relies on it now.
- What Tuwunel answers a locked or deactivated account: Task 2.5 (already planned).

## Design-text fixes to propose

- B 2.3.2 and C 3.7.3 could say the marker read is `found=false` only on `M_NOT_FOUND` (finding 4).
- B 2.6 step 09 / A 6.6: the first sync after an outage uses the default `set_presence`, which
  marks the member online by the specification; say so beside HC-A1.
