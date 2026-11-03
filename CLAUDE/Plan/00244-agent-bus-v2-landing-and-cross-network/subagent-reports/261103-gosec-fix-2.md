# Task 8.2, part 2: the last G304 and G101 sites fixed in code

Part 1 is `CLAUDE/Plan/00209-agent-bus-v2-in-go/subagent-reports/261103-gosec-fix.md`. The
owner's decision on pull request 124 stands: every G304 site is fixed in code and none is
recorded; the lxc tests' G101 likewise. `src-go/.golangci.yml`, `qa/record.toml` and
`qa/go-qa.toml` were not touched, and there is no `//nolint` or `#nosec`.

Status: every site is fixed. Nothing is left as a reported finding.

## Re-measure

`golangci-lint run ./internal/teamchat/...` before this part: 13 findings, all gosec, in the
three builds alike (7 G304, 6 G101). They were the sites part 1 listed, and no others.

## What changed (all test files, plus one helper)

| Site                                                                  | Fix                                                                                                                              |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `member/proof/scan_test.go` `appendTo`                                | `testfiles.OpenFile` on the file's own directory (every caller's file is in the directory `transcript(t, ...)` made)             |
| `member/wiring/e2e_world_test.go` `copySelf` (two sites)              | `testfiles.Executable` for the source; `testfiles.OpenFile` on `w.dir` for the copy (`O_CREATE\|O_EXCL`, 0700)                   |
| `member/wiring/e2e_world_test.go` `session.log`                       | new `testfiles.Try(dir, name)` (below), on the session's runtime directory                                                       |
| `host/lxc/absent_test.go` `holdSeat`                                  | `testfiles.Open` on the box directory, the guest path without its leading slash                                                  |
| `host/lxc/probe_test.go` `holdFile`                                   | takes `(t, dir, name)` and `testfiles.Open`; its one caller passes the box directory and the lock's guest path without its slash |
| `host/lxc/world_test.go` `guestFile`                                  | `testfiles.Find` on the box directory; the not-exist answer is kept                                                              |
| six G101 findings (`etc/passwd` as a map key in `writeTree` literals) | `guestUsersFile = "etc/passwd"` in `world_test.go`, used as the key at all six sites and at the other sites that spell the path  |

`testfiles.Try(dir, name) ([]byte, error)` is `Read` that never ends the test. `session.log`
is a diagnostic (it builds the failure text of a wait), so a read error must come back as text
and not replace the failure it is describing; `Find` would have ended the test on any error
but not-exist. It is one function in the shared helper, with its own test (red first: `Try`
undefined; green after), not a copy in the wiring package.

G101 fires on the map key string literal `"etc/passwd"` (its name matches the rule's credential
pattern) beside a string literal value. A constant named `guestUsersFile` as the key clears it,
confirmed by the linter in all three builds, and keeps the meaning at the call site.

## Checks, from `src-go`

- `gofmt -l internal/teamchat`: empty. `go build ./...`: clean.
- `go vet ./internal/teamchat/...` untagged, `-tags busfailpoints`, `-tags tuwunel`: clean.
- golangci-lint 2.14.0 over `./internal/teamchat/...`: 0 issues untagged, with
  `--build-tags busfailpoints` and with `--build-tags tuwunel` (no G304, no G101, nothing new);
  also 0 for `--build-tags teamchatrelease` over `./internal/teamchat/member/cli/`.
- Project rules driver (`go -C qa build ... ./driver`, then `go vet -vettool=<driver> ./internal/teamchat/...`): nothing reported untagged, with `busfailpoints` and with `tuwunel`.
- `go test -race -shuffle=on -count=1` passed: `testfiles`, `member/proof`, `member/wiring`
  and `host/lxc/...` (all its packages); and `-tags busfailpoints` for `member/proof`,
  `member/wiring`, `host/lxc`. The `tuwunel` tests were vetted and linted, not run (they need
  a Tuwunel binary).
- Not run, by instruction: `scripts/qa-all.bash`, `scripts/qa-changed.bash`.

## Quiet without being bounded (reported, not changed)

Non-test opens under `src-go/internal/teamchat` whose path is a caller's and which pass G304
only because of a `filepath.Clean` or an `os.DirFS` around it. `Clean` is lexical: it neither
refuses a `..` nor a link, so it bounds nothing. The "by" column says who added the form.

| File:line                                     | By      | Why it is not bounded                                                                                                                                                                                                                                         |
| --------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `declare/hostvars/hostvars.go:221`            | part 1  | `os.DirFS(filepath.Clean(dir)).Open(name)`: the caller's whole path; `name` is one element, `dir` is any directory. Following links is by design. No non-test caller yet.                                                                                     |
| `state/state.go:29`                           | part 1  | `os.OpenRoot(filepath.Clean(dir))` for `WriteFile`: the root is the caller's directory, so the write is bounded to that directory and one generated temporary name, but the directory itself is any.                                                          |
| `state/state.go:110`                          | part 1  | `unix.Open(filepath.Clean(dir), O_PATH\|O_DIRECTORY)` for the readers: not a gosec G304 call at all, so the `Clean` quiets nothing; the bound is the directory handle and one name (`ReadOwnedTarget` follows a link out of it on purpose, part 1 finding 2). |
| `host/dirs.go:51`                             | earlier | `os.OpenFile(filepath.Clean(dir), O_DIRECTORY\|O_NOFOLLOW)` in `mkdirMode`: the caller's whole path; `O_NOFOLLOW` refuses a link in the last element only, a link in a parent is followed.                                                                    |
| `host/dirs.go:78`                             | earlier | `os.OpenFile(filepath.Clean(temp), O_CREATE\|O_EXCL\|O_NOFOLLOW)` in `createOnly`: the caller's whole path (a generated name in the caller's directory), same last-element-only limit.                                                                        |
| `host/dirs.go:100`                            | earlier | `os.Open(filepath.Clean(dir))` in `syncDir`: the caller's whole path, no `O_NOFOLLOW` at all.                                                                                                                                                                 |
| `host/lxc/lxcstate/lxcstate.go:508`           | earlier | `os.OpenFile(filepath.Clean(dir), O_DIRECTORY\|O_NOFOLLOW)` in `mkdirExact`: as `host/dirs.go:51`.                                                                                                                                                            |
| `member/faketranscript/faketranscript.go:257` | earlier | `os.OpenFile(filepath.Clean(path), O_CREATE\|O_EXCL\|noFollow)` in `create`: the caller's path (built by `pathFor` from a directory and a plain session id); last-element-only `noFollow`.                                                                    |

Also quiet, and not rewritten: `member/faketranscript/faketranscript.go:131` and `:229` open
`t.path`, a struct field. gosec's G304 does not follow a selector expression, so these pass
without any bound beyond `noFollow` on the last element.

The other non-test `os.ReadFile`, `os.ReadDir` and `os.OpenRoot` sites (`member/link/proc.go`,
`member/keeper/osproc.go`, `reexec/reexec.go`, `host/lxc/kit.go`, `health/files.go`,
`host/apply.go`, `host/lxc/restart.go`, `hostnet/network.go`) build the path from a pid or a
configured root and a literal, or open an `os.Root`; none was rewritten to quiet the rule.
