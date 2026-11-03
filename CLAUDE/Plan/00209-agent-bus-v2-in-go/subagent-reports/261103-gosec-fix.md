# Task 8.2: the G304 and G101 exclusions fixed in code

Status: part 1 of 2. The first builder stopped at its context budget with the work below
committed. The rows marked LEFT and the "Left to do" list were finished in part 2, whose report
is `CLAUDE/Plan/00245-agent-bus-v2-landing-and-cross-network/subagent-reports/261103-gosec-fix-2.md`
(with the final commands and results, and the sites that are quiet without being bounded).

The owner's decision on pull request 124: fix every G304 site in code, do not record it; if one
cannot be fixed, say why in the pull request. The same was applied to the one G101 exclusion on
the lxc phase tests. `src-go/.golangci.yml` already lacks the G304 and G101 codes for the eight
entries (the red step), so the linter's findings list over `./internal/teamchat/...` is the
sweep.

## What was done

Sweep, untagged, `--build-tags busfailpoints` and `--build-tags tuwunel` alike: 36 findings (30
G304, 6 G101), the same in all three builds.

| Entry                                                                                                | State                                                                                                                                                           |
| ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `state/state.go` (3 sites)                                                                           | fixed, see the two designs below                                                                                                                                |
| `teamlock/teamlock(_test).go`                                                                        | production file already clean (it opens by `unix.Openat` on its directory handle); the test's `os.ReadFile(filepath.Clean(path))` replaced by the shared helper |
| `declare/hostvars/hostvars.go`                                                                       | fixed, `os.DirFS(dir).Open(name)`                                                                                                                               |
| `element/*_test.go` (1 site)                                                                         | fixed                                                                                                                                                           |
| `seat/*_test.go` (3 sites, about 60 call sites)                                                      | fixed, all three builds                                                                                                                                         |
| `member/*_test.go`: homefs (3), guest (1), faketranscript (1), deliver (1), lock (1), statusline (1) | fixed                                                                                                                                                           |
| `member/kit/*_test.go` (7 sites)                                                                     | fixed                                                                                                                                                           |
| `member/*_test.go`: proof (1), wiring (3)                                                            | LEFT                                                                                                                                                            |
| `host/lxc/*_test.go`: G304 (3), G101 (6)                                                             | LEFT                                                                                                                                                            |

## The helper

`internal/teamchat/testfiles` (new, with its own tests): `Read`, `Find` (no file is not a
failure), `Open`, `OpenFile`, `Executable` (the running test binary, opened in its own
directory, as `admin/execchild_os_test.go` already does). Each takes the directory the test owns
and a name under it, relative or a full path under that directory, and opens through an
`os.Root`, so a `..`, a full path elsewhere or a link that leads out is refused. The tests prove
the refusals, and were seen red against an unbounded `os.ReadFile`.

## The experiment: does gosec accept `os.Root`

Yes. With golangci-lint 2.14.0 and this module's config, G304 fires on `os.ReadFile(path)` and on
no `os.Root` form: `root.OpenFile` (read, and with `O_CREATE|O_EXCL`), `root.Open`,
`root.ReadFile`, nor `os.OpenRoot(dir)` with a variable `dir`. No suppression was used.

## Findings the owner should know

1. **`os.Root` cannot refuse a final link.** `Root.OpenFile` adds `O_NOFOLLOW` itself and, on
   `ELOOP`, follows the link when it stays inside the root, whatever flags the caller passes
   (run, not only read: an in-root link opened with the caller's `O_NOFOLLOW` was opened). Spec
   ST2 and the state tests require `ReadFile` to refuse a link. So the readers (`ReadFile`,
   `ReadOwnedFile`, `ReadOwnedTarget`) open the last element with `openat` on an `O_PATH` handle
   of its directory, the form `teamlock.go` already uses: one name that cannot lead out of the
   directory, and the kernel refuses a link in the same call. `WriteFile` uses an `os.Root`
   throughout (the temporary file, the rename, the removal and the directory sync); a root
   creating a file exclusively never follows a link.
2. **`ReadOwnedTarget` follows links out of its directory on purpose** (os-release is a link to
   a file in another directory). No `os.Root` could hold that, so the bound it has is the
   directory handle and the single name, not the link's destination. If the owner counts that as
   unbounded, this one open is the site to leave as a reported finding.
3. **`hostvars.ReadFile` follows links on purpose** (a checkout may keep `host_vars/<host>.yml`
   as a link to a file elsewhere). `os.DirFS(dir).Open(name)` bounds the name lexically
   (`fs.ValidPath`) and keeps the link following; a new test pins it.
4. **The same class, not found by gosec, left alone.** `filepath.Clean` makes gosec's G304 pass
   without bounding anything. Outside the eight entries, in `internal/teamchat`: production
   `host/dirs.go` (lines 51, 78, 100), `host/lxc/lxcstate/lxcstate.go` (508),
   `member/faketranscript/faketranscript.go` (257); tests `apply/member_retry_test.go` (139),
   `failpoint/failpoint_test.go` (90), `health/host_test.go` (31), `health/files_test.go` (133),
   `host/fakes_test.go` (57). The owner decides whether a rule for it comes first.
5. New state tests: a path with no file name (read refused, write fails and leaves no temporary
   file) and a file in a search-only directory (`O_PATH`, not `O_RDONLY`, on the directory). The
   second only discriminates for a non-root user; it was run as an unprivileged user with
   `setpriv`, red with `O_RDONLY` and green with `O_PATH`.

## Left to do

- `member/proof/scan_test.go` `appendTo` (line 45).
- `member/wiring/e2e_world_test.go`: `copySelf` (lines 202, 205) and `session.log` (line 520).
- `host/lxc`: `absent_test.go:46` (`holdSeat`), `probe_test.go:182` (`holdFile`),
  `world_test.go:728` (`guestFile`; a guest path starts with a slash, strip it); the six G101
  findings are the map key literal `"etc/passwd"` in `writeTree(...)` calls (apply_test 282 and
  286, faults_test 200 and 208, seats_test 202, world_test 106): name the path in a constant
  whose name has none of pass, pwd, secret, token, cred, pw, and use it as the key.
- Then the checks: golangci-lint over `./internal/teamchat/...` untagged, `busfailpoints` and
  `tuwunel` (no G304 and no G101 expected); `go test -race -shuffle=on` for every touched
  package, `-tags busfailpoints` where there are tagged tests; `go vet` in the three builds; the
  project rules driver (`go -C qa build -o ../untracked/scratch/driver ./driver`, then
  `go vet -vettool=$PWD/untracked/scratch/driver ./internal/teamchat/...`); `gofmt -l`.

## Checks run so far

- golangci-lint 2.14.0, red step: 36 findings, identical untagged, `busfailpoints`, `tuwunel`.
- `go test -race -shuffle=on -count=1` passed for: state, declare, declare/hostvars, teamlock,
  element, seat, testfiles, member/{homefs, deliver, guest, faketranscript, kit, lock,
  statusline}, and (before the helper work) admin, host, hostnet, health, apply, status.
- `go vet` over `./internal/teamchat/...`, untagged, `busfailpoints`, `tuwunel`: clean.
- `gofmt -l internal/teamchat`: clean.
- golangci-lint over the packages done: 0 issues (state, declare, testfiles, element, teamlock,
  seat untagged and tuwunel, homefs, kit, guest, faketranscript, deliver).
- Not run yet: the project rules driver; the full golangci-lint sweep after the last sites.
