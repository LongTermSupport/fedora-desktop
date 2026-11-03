# Task 8.8 slice S10: site and hostname (X25 to X28)

## Commits (on the worktree branch, after the merge of the cross-network branch)

1. `project_hostname_read` (X27): analyzer, fixture, `All()` (24 rules), catalogue entry.
2. X25 in guestfacts: `Prefix` no longer reads `ccy.env.local`.
3. X25 and X26 in declare: `teamchat_site`, `CheckSites`, `MachineHostname`, the plan journal entry,
   this report.

## Red evidence

- X27: `TestHostnameRead` against an analyzer that reports nothing: 19 missed diagnostics (the
  package with a test file is judged twice). `TestAllIsValid` with 24 wanted: "want 24 rules, got
  23". Green once the rule and the `All()` entry were written.
- X25 (guest): `TestPrefixSite` with the env cases changed to expect `local`: `repo+host-a.lxc`
  for the plain and the indented case, and a refusal for a malformed env file. Then the parser was
  removed. `TestNoSourceNamesTheHostnameVariable` was written after the removal (green on arrival).
- X25 (declare) and X26: `site_test.go`, `machinehost_test.go` and `TestReadTheMachinesSite` were
  written before the code; the red was a build failure (`Site`, `CheckSites`, `SiteIsAHostname`,
  `MachineHostname` undefined), not a failing assertion.

## Sweep of the real tree

Driver over `./internal/teamchat/...` and the two commands, untagged, with `busfailpoints` and with
`tuwunel`: no finding, before and after the exempt file existed. No instance of `os.Hostname` or
`net.LookupAddr` was in the tree; the defect it stands for (the env-file read) is fixed in commit 2.

## Design decisions

- Where the per-machine value lives: host_vars `teamchat_site`, a sibling of `teamchat_address` in
  `declare.Declaration` (`Site`). An LXC entry that names no `site` takes it when the declaration is
  read, so the entry the apply sees already carries it; with neither, `guestfacts.DefaultSite`
  (`local`) applies where the handle is made. The entry's own `site` still wins, validated as before.
- X26 is `Declaration.CheckSites(hostname func() (string, error))` in `declare/sitecheck.go`; the
  hostname is asked only when a site is declared, a seam error or an empty name fails the check, one
  fixed message (`declare.SiteIsAHostname`) says it is best effort. `declare/machinehost.go`
  (`MachineHostname`) is the one file that reads the hostname.
- X28 is in the `declare` package doc only.

## Owner questions

1. The exemption of `project_hostname_read` is the file `internal/teamchat/declare/machinehost.go`,
   part of the rule's own definition (not a row of an allowance table). Confirm the file.
2. The rule also judges `cmd/teamchat` and `cmd/teamchat-admin` and test files, wider than the
   spec's "under teamchat/". Confirm, or narrow.
3. `lxc/apply.go` still reads `ccy.env.local` for its owner check (`TestTwoUsersShareACheckout`
   pins it) although the file's content feeds nothing now. Drop the check?
4. An entry's own site is held to the url host labels of every present team, not only its own:
   conservative. Confirm.

## Left (outside this slice's files)

- `CheckSites` has no caller: it must be called, with `declare.MachineHostname`, where a declaration
  is read in production (`admin/lxc.go`, `admin/verbs.go`, the ccy-teams reader). Those packages were
  off limits; until it is wired X26 is not enforced at run time.
- `declare.HostVarSite` belongs beside the other host_vars names in `paths` (off limits here).
- Other agents adding rules will also change the `24` in `TestAllIsValid`: a merge conflict to
  resolve by count.
- Docs: `teamchat_site` is not yet in the user-facing documentation or the Ansible host_vars
  examples (the docs agent's files).

## Checks (from src-go)

- `gofmt -l` on the changed files: empty. `go build ./...`: ok.
- `go vet ./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...`, and the two tags
  over `./internal/teamchat/...`: no output.
- `golangci-lint run ./internal/teamchat/...`, and with each tag: no output.
- Project rules through the driver, untagged and with each tag: no output.
- From `qa`: `go test ./rules/...`: ok.
- `go test -race -shuffle=on -timeout=25m ./internal/teamchat/...`: 64 packages ok, no failure.
- The tagged test runs (`busfailpoints`, `tuwunel`) were not run; those tags were vetted and linted.
