## 00:48 · action · —

Sealed carry item 08 (SPEC-carry section 2), green.

`go test -race` over carry, admin, admin/wire and ctl passes, golangci-lint reports nothing.
One test failed after the code first built: TestExportRemovesALeftPart compared exports of two
worlds, which differ because each world makes its own CA key; it now compares within one world.

Built: `carry.Export` (SX1-SX7: archive cap read first, SealedGuard under the lock with
host.PlayWait, a digest pass that builds the manifest and a write pass that checks it again, the
carry directory made as backup's is, a left `.part` removed after a Stat shows a regular file, an
existing export replaced only by one of equal digest, else 1 naming both digests, SX6's answer);
`carry.Files` and its host implementation `carry.OS` (sources opened by openat2 with
RESOLVE_NO_SYMLINKS); `Deps.Files` and `Deps.MaxArchive`; `carry.fits`, the archive check that
both WriteArchive and Export use; `carry.AdminTokenName`, which admin's name now refers to;
`admin.CarryMaxArchive` (CN5, every failure 78 naming the setting); the `team export` verb,
which shares `carryVerb` with `team seal`; wire sets Files and MaxArchive.

SPEC-carry SC5 now says the server name comes from the `server-name` file, not `units.json`.
