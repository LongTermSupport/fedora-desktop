# vault-list: the vault helper named by a constant (project_seam_commands)

GitHub CI's go-qa analyzers stage failed on batch build cb8848805 with one finding:
`project_seam_commands` at `internal/vaultlist/vaultlist.go:367`. There `encryptAll` named its
program as `filepath.Join(r.root, "vault.bash")`, so the Name was not a compile-time constant.

## Fix

The program is not chosen at run time. It is always the checkout's `vault.bash`, and the command
already runs with `Dir: r.root`. So the fix is a constant, not a `dynamicNames` row. The new
`vaultHelper = "./vault.bash"` is run with `Dir` set to the checkout.

- os/exec does not look a name holding a slash up on PATH, and starts a relative one relative to
  `Cmd.Dir`. `execx.OS.Run` hands `Name` and `Dir` to `exec.CommandContext` unchanged, so it
  keeps that behaviour.
- `vault.bash` is not in the owner table, so any package may run it.
- ftpcamera's `cli.execVault` has a `dynamicNames` row because its helper path comes from a
  config file. That case does not apply here: vault-list's helper is fixed.

## Tests (test first)

- The fakes in `vaultlist_test.go` now match `./vault.bash`. The existing V1 test already
  checks that every command runs in the checkout (`c.Dir == w.root`).
- New test `TestTheVaultHelperRunsFromTheCheckoutNotTheWorkingDirectory` runs `vaultHelper`
  through the real `execx.OS` from a different working directory, with `Dir` set to a scratch
  checkout that holds a stub `vault.bash`. It proves the relative name is resolved against `Dir`.
  Before the constant existed, the test did not compile (red).
- `go test -race ./internal/vaultlist/... ./internal/hostvars/...`: ok.
  `golangci-lint run` on both: 0 issues.

## Analyzers

- Project-rules driver (`go -C qa build ./driver`, run as `go vet -vettool`):
  - on `internal/vaultlist` at HEAD (extracted with `git archive`): red, the CI finding;
  - on the fixed tree: 0 findings.
- Over every package changed since `origin/F44`: 22 src-go packages, and the qa module's
  `ansible`, `bashlimits`, `cmd/qa-private-names`, `internal/semgrepreport`, `privatenames` and
  `rules`. Result: 0 findings in each module.
- go-qa's own analyzers stage, the way `scripts/qa-go-qa.bash` runs it
  (`go -C src-go/qa tool go-qa --root src-go run --no-fix --changed --base origin/F44 -t analyzers`).
  A change to `qa/ansible/checkouthome.go` widened its scope to the whole project. Result: one
  finding, and it is not in the committed tree:
  `project_seam_commands` at `internal/teamchat/member/guest/find.go:160`. There `Shell.ask`
  runs `Name: shell`, the login shell it found for the seat user. That file is untracked work
  in progress by the teamchat agent. That agent or the coordinator must fix it before it is
  committed: either a `dynamicNames` row whose reason says the program is the seat user's
  login shell, or a constant. Until then the next batch build will fail the same stage.
  vault-list's finding is gone.
