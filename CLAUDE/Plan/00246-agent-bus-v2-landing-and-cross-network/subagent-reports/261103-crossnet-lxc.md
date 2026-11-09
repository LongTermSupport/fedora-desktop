# Task 8.8, slice S3: the LXC guest source check judges the effective networks (X5)

Written by the colleague who built S3; the text below is its notes as they stood, carried here so
the evidence is in the plan folder. Both commits are in the history of the cross-network branch.

## Commits

- `1169b9135` the red tests: three tests in `host/lxc/apply_test.go`; in `world_test.go` the
  `world.networks`, `bridge` and `bridgeErr` fields and `Deps.BridgeNet` in `deps()`; in `lxc.go`
  the `Deps.BridgeNet` field, unused.
- `370a2729b` the fix: `guestfacts.Source(addrs, []netip.Prefix, link, bridge)` with the message
  "inside the team's effective networks"; `lxc.run` keeps each acted team's `declare.Input` in
  `inputs`, and `facts()` computes `r.inputs[team].EffectiveNetworks(r.d.BridgeNet)` lazily, so
  the absent phase never asks the bridge; `wire/lxc.go` sets
  `BridgeNet: hostnet.Network{Proc, BridgeName}.Bridge`. The test went to `apply_test.go`
  (`world_test.go` holds helpers and no Test functions).

## Red evidence (`untracked/scratch/red-x5.txt`, not committed)

- `TestGuestOnTheBridgeIsInsideTheEffectiveNetworks` failed with "refused: the guest's address:
  no address of the container is inside the team's allow_from".
- `TestGuestOutsideTheEffectiveNetworksIsRefused` failed on the same old message.
- `TestBridgeSubnetFailureFailsTheMember` failed with "err <nil>".
- Green after the fix: `untracked/scratch/green-x5.txt`.

## Checks (src-go)

- gofmt, go build, go vet (default, `busfailpoints`, `tuwunel`): clean.
- golangci-lint 2.14.0, three builds: 0 issues each; the 13 gosec findings the S1 and S2 report
  named are gone.
- Project rules driver, three builds: nothing.
- `go test -race -shuffle=on -timeout 25m ./internal/teamchat/...`: 63 packages ok, no FAIL.

## Defence Before Fix

The class "a consumer reads the declared networks, not the effective set" has no analyzer: a rule
is a `src-go/qa/rules` edit, which this task's agents may not make. Sweep: no non-test reader of
`Team.Networks` remains outside package `declare`. Owner question: add the project rule.
