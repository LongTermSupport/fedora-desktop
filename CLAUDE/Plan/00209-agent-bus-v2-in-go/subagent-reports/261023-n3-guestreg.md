# n3-guestreg: guest TestRegister repeatability

## Cause

`TestRegister` called the package's `register` with the verb `guest register-test`. `register`
adds to the package-level `registry`, which outlives the test, so the second run in one process
(`-count=2`) hit the duplicate-verb panic. Production is not affected: `register` runs once per
op at package var initialisation, and the panic on a duplicate is its intended guard.

## Class (Defence Before Fix)

A test that cannot run twice: it adds to package-level state it does not restore.

## Fix

`output.go`: the registry is an `opSet` type (`add`, `all`, `changesOf`); the package holds one
(`registry`), and `register`, `Ops` and `ChangesOf` delegate to it. `TestRegister` builds its own
`opSet`. `TestRegisterIsThePackageSet` covers the delegation. Behaviour of the exported API is
unchanged.

## Defence

`src-go/qa/go-qa.toml`: the `teamchat-failpoints-test` stage now runs with `-count=2`, so every
teamchat test runs twice in one process under `-race -shuffle=on`. It is the existing stage, not a
new one, and go's test cache still makes an unchanged teamchat a cache hit. The busfailpoints
build is a superset of the untagged one, so it covers both. Documented in `CLAUDE/QA.md` and
`src-go/CLAUDE.md`.

Measured: `go test -count=2 ./internal/teamchat/...` took 2m06s untagged (cold cache) and 2m20s
with busfailpoints. A changed teamchat therefore costs about double its single run in that stage.

## Sweep

`go test -count=2 ./internal/teamchat/...`, untagged and with busfailpoints: `guest` was the only
failure. The tuwunel build was not swept (it needs the binary and no gate runs it).

## Proof

`-race -shuffle=on -count=3` green for `member/guest`, untagged and with busfailpoints; coverage
93.9%; golangci clean untagged, tuwunel and busfailpoints.
