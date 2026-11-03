# Task 8.8, slices S1 and S2: networks declared, listen addresses derived (report 2)

Continues the first agent's networks work (its merged WIP commit). S3 (the lxc source check) and
the docs rewrite are not part of this report's commit.

## Commits

- Merge of the first agent's WIP (`Merge the networks WIP`), no conflicts.
- One work commit with S1 and S2 together (they cannot build apart: `Team.Listen` and
  `Team.AllowFrom` are gone). Formats break because v2 is not deployed.

## What was done

- S1 (declare): `gate.go` (Gated.Networks replaces AllowFrom and Listen, `networks` a gated list
  field with `--add`/`--remove`, `approved.json` read through `fileVersion` so a version 1 file is
  refused naming its version); new `declare/networks.go`: `Input.EffectiveNetworks(bridge)`
  (declared networks, the bus address as a host prefix, the bridge subnet only while a present LXC
  member needs it; sorted, each once; `Own` is the bus prefix and the bridge).
- S2 (host): `host/networks.go` (`listenAddress`, `parseAddrs`); `apply.go` derives everything in
  step 2 (`deriveNetworks`, `serve`, `urlHost`): the listen set, the device units, the rules, the
  filter and units.json all read `a.networks` and `a.listen`; `render.go` takes `listen` and
  `networks`. `allowed()` and the declared-list reads are deleted. The hostnet `Route` piece was
  already in the WIP.
- lxc compiles by passing the DECLARED `m.team.Networks` to `guestfacts.Source` (S3's red test
  shows against this; `guestfacts` text still says `allow_from`).
- Fixtures: every `"listen"`/`"allow_from"` fixture is now `"networks"`; the fake network got a
  route table and `Route`; `bindings` in the fake reads the installed config's `address` line.

## Red evidence (untracked/scratch, not committed)

- X1: `red-declare.txt` (first agent, compile red before the source). Two of the first agent's
  `networks_test.go` cases were wrong (the default fixture holds an LXC member, so the bridge was
  asked); fixed in the test, the intent in its comments.
- X2: `red-x2.txt`: with `listField` not naming `networks`, `TestCompareEachGatedField` fails for
  network added, replaced, emptied and in AG1 order.
- X4: `red-host.txt` (compile red before the source); `red-x4.txt`: with the filter not reading the
  effective networks, `TestASecondNetworkChangesOnlyWhatItShould`, `TestReloadSetOnly`,
  `TestRenderTheTeam`, `TestDigestSets` and others fail.
- X6: `red-x6.txt`: with the url check disabled, the refusal rows for 192.0.2.77, loopback and the
  undeclared second interface fail.

## Checks (src-go)

- `gofmt -l internal/teamchat`: empty. `go build ./...` and `go vet ./internal/teamchat/...`
  (default, `busfailpoints`, `tuwunel`): clean.
- `go test -race -shuffle=on -timeout 25m ./internal/teamchat/...`: 63 packages ok.
- golangci-lint 2.14.0 over `./internal/teamchat/...` (default and both tags): 13 gosec
  findings (G101, G304) in test files under `host/lxc`, `member/proof` and `member/wiring`, none in
  a file this change owns; they are the gosec fixes' to clear. Nothing else.
- Project rules (`go vet -vettool=driver`, default and both tags): nothing.

## Decisions for the owner

01. The X6 literal reading refuses a loopback IP-literal url (127.0.0.1 is bound but is not a
    derived listen address): a loopback-only team uses a DNS name or the bus address. Confirm.
02. A network no route covers is refused naming the CIDR (the spec is silent).
03. The bus address /32 is routed by the interface that holds it (the local table is not in
    `/proc/net/route`); `Firewall.Zone` still reads the main table only, so that rule's zone is the
    default route's interface zone. Unchanged here; say if the zone should follow the holder.
04. An IPv6 link-local address is never chosen as a listen address (it needs a zone).
05. The effective set is sorted: the filter and rule order differ from declared order plus bridge.
06. The bridge is now a derived network: a team with an LXC member also listens on the bridge's
    address (X3 read literally) and gets that device unit. Confirm that is wanted.
07. The bridge's failure is now a step 2 (refusals) failure, where it was step 3 (render), because
    the set is derived where a route is refused.
08. A wildcard listen refusal is gone: listen addresses now come from an interface, which never
    holds one.
09. The firewall now has a rule for the bus address /32 besides each declared network.
10. Defence Before Fix: the class "a consumer reads the declared list, not the effective set" has
    no analyzer (a project rule would be a rule-table edit, forbidden to this agent); the X4 golden
    tests are the proof. An AST test denying `.Networks` outside package declare was not written.

## Left

- S3: the lxc source check (`guestfacts.Source`, `BridgeNet`, its red test against the declared
  list), and its message text.
- The docs rewrite (`listen`/`allow_from` as declared fields in Plan 00209 design docs and docs/).
- The plan file needs no change: Task 8.8 is one task.
