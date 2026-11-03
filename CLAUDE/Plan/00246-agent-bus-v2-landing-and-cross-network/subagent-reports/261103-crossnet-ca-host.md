# Task 8.8, slice S7a: the host keeps the team's CA and the homeserver leaf

Branch `agent-a0de35cf695950ea2-7008fda8`, after a no-fast-forward merge of `spike/v2-cross-network`
(no conflict). S7a (X13, X14 at the host) is done except the backup tar itself, which is not built
(see "Found"). The terminator unit (S7b) is not touched.

## Commits

| Commit      | What                                                                                                                                                                  |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `8b3d9983e` | `paths.TLSDir`, `CACertFile`, `CAKeyFile`, `LeafCertFile`, `LeafKeyFile` (`root/<team>/tls/{ca.crt,ca.key,leaf.crt,leaf.key}`), with the three paths tables           |
| `67ead151c` | `host/tls.go`: step `tls` in `team apply`, `CAPEM`, `CABackupFiles`, `RestoreCA`, `BackupFile`; `host/tls_test.go`; the test hosts' randomness; `admin/world_test.go` |
| `7a8aa7157` | `DESIGN-v2-C-host.md`: the `tls/` row, the "team's certificates" paragraph after the apply steps, the CA in backup step 3                                             |

## What it does

- Step `tls` (`host.StepTLS`) runs after step 4's files and before the firewall, every apply. Time is
  `Deps.Clock` (adapted to `pki.Clock`), randomness `Deps.Rand`.
- CA: absent means made (key first, then certificate, each `createOnly`, so nothing is ever replaced),
  `ca made`. Present means parsed with the key (`pki.Fingerprint` for the certificate, `pki.ParseCA` for
  the pair). Unparseable, mismatched or a lone file is `Error{Step: tls, Drift: true}` (exit 70) naming the file,
  before the firewall, the unit or bootstrap run. A link or non-regular file is refused by `state.ReadFile`.
- Leaf: names are every derived listen address plus the `url` host when it is a name. Made when absent
  (`leaf made`), or again when pki says so, or when it does not verify against the CA (missing half, torn pair,
  another CA): `leaf made again: its names changed` / `fewer than 30 days remain` / the pki phrase. Key written
  before the certificate, both atomically through `state.WriteFile`. Nothing reported on a second apply.
- Modes: `tls/` 0700 (converged each apply), both keys 0600, both certificates 0644.
- `host.CAPEM(root, team)`: the CA certificate PEM, read as root and judged with its key; "not made yet" if absent.
- `host.CABackupFiles(root, team)`: `ca.crt` (0644) then `ca.key` (0600), never the leaf; nothing for a team with
  no CA; an error for a CA the host cannot use. `host.RestoreCA(root, team, files)`: exactly those two names, once
  each, with those modes, a pair pki accepts, each under the 1 MiB cap; writes only when the team has no CA; the same
  CA is a no-op; another CA is refused; no files at all is a no-op.

## Found

The backup tar of C 3.12 is not built (`wire.ErrBackupNotBuilt`, `NoBackups`): there is no tar writer or parser to
add the CA to. S7a therefore gives the tar writer and parser the functions and the member names to call, and the
design now lists them. Wiring them in is part of building 3.12.

## Red evidence (files under `untracked/scratch/`, not committed)

- paths, `s7a-red-paths.txt`: 10 x `undefined: TLSDir`, `CACertFile`, `CAKeyFile`, ...
- host, compile red first (`s7a-red-host-compile.txt`): `undefined: StepTLS`, `CABackupFiles`, `BackupFile`.
- host, behavioural red with stubs and no `tls` step (`s7a-red-host.txt`): 16 tests failed, among them
  `TestFirstApply`, `TestFirstApplyMakesTheCAAndTheLeaf`, `TestAChangedNetworkMakesOnlyTheLeafAgain`,
  `TestALeafNearExpiryIsMadeAgain`, `TestACAThatCannotBeUsedIsRefusedAndNeverReplaced` (7 rows),
  `TestTheBackupCarriesTheCAAndNotTheLeaf`, `TestRestoreRoundTrip`, `TestCAPEM`.
- Mutations of the finished code, each seen red and restored (`git status` clean after): key mode 0644 failed
  5 tests (`ca.key: mode -rw-r--r--, want -rw-------`); `loadCA` swallowing a bad key failed 3 tests (and
  `createOnly` still refused to replace: `file exists`); renewal ignoring expiry failed
  `TestALeafNearExpiryIsMadeAgain` (`29 days before the end: lines []`).

## Checks (from `src-go`, all green on the final tree)

- `gofmt -l` on host, paths, admin: empty. `go build ./...`, `go vet ./...`, `go vet -tags busfailpoints`
  and `-tags tuwunel` over `./internal/teamchat/...`: clean.
- `go test -race -shuffle=on -timeout 25m ./internal/teamchat/...`: 64 packages ok. `-tags busfailpoints -count=2`
  over host and paths: ok.
- golangci-lint 2.14.0 over `./internal/teamchat/...`, plain, `--build-tags busfailpoints`, `--build-tags tuwunel`:
  0 issues, no suppression, no config change.
- Project rules driver (`go -C qa build ./driver`, `go vet -vettool=...`) over `./internal/teamchat/...`, plain
  and both tags: nothing.
- Not run, as told: `qa-all.bash`, `qa-changed.bash`.

## Fixtures changed on purpose

- Every host test now makes a CA and a leaf, so the test host's randomness is the registration key's 64 bytes then
  an endless stream (`stream` in `host/fakes_test.go`); the old reader ran dry. `TestFirstApply`'s CHANGED lines
  gain `ca made`, `leaf made`. `admin/world_test.go` gives its host the same 64 bytes then `crypto/rand`.

## Wiring for slice S6 (not done: S6 is not merged)

When `admin.SeatBundles` has its CA provider field, the production line in `admin/wire/wire.go` (where it builds
`admin.SeatBundles{Clock: clock.Now, Digest: d}`, which has `root` in scope) becomes the one-line replacement of the
"the team CA is not made yet" placeholder with `func(team paths.TeamID) (string, error) { return host.CAPEM(root, team) }`.

## Owner questions

1. CA lifetime: ten years (X13), no early renewal. After that the leaf step fails ("the CA is not valid at this time")
   and renewing the CA means reissuing every bundle. Nothing watches for it; say if a warning is wanted.
2. Backup inclusion (X13): built as the default says. Restore puts the CA back only when the team has none, never over
   another one.
3. Refusal on a bad CA: an apply fails with exit 70 naming the file. The owner's way out is to restore the file or remove
   both files for a new CA (all bundles then reissued); there is no verb for it. A crash between the two CA files leaves
   a key alone (it signed nothing) that the next apply refuses by name. Say if a repair verb is wanted.
4. The leaf certificate is mode 0644 like the CA's (a certificate, in a 0700 directory); say if 0600 is wanted.

## Left

- S7b: the terminator reads `leaf.crt` and `leaf.key` as credentials. The leaf is not in `units.json`'s digest sets, so
  a renewed leaf does not yet restart or reload anything; S7b decides that.
- The tar writer and parser of C 3.12 call `CABackupFiles` and `RestoreCA`.
- No Defence Before Fix rule: nothing here fixes a defect. A class worth a detector later is "a `.key` file written
  with a mode over 0600 under `teamchat/`"; it needs a rule table edit, which I was told not to make.
- PLAN.md is untouched: Task 8.8 is one open task line; the coordinator records slices.
