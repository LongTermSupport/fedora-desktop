# rclonetail and project_realtime_bound

Branch `agent-af6d5f14f2f32c8fb-06029089`, tip `e96de635f`, based on F44 `1d8bc1037`
(`e8f8973ea` is not an ancestor).

## Finding

`src-go/internal/rclonetail/app.go:60` is `NewEnv` handing `time.After` to `Env.After` as a
value. `Env.After` is already the injected clock seam: the only wait (`app.go:189`, between
ticks) goes through it, and every test builds its Env from `world.newEnv`. That function
replaces `After` with a fake that fires at once, or never fires after a stop. `TestNewEnv`
only checks that fields are not nil and never waits. This is the "real side of a clock seam
whose tests replace it whole" case, the same shape as the existing `internal/wsi` and
`internal/wsimedia` `NewEnv` rows. The code needed no change.

## Fix

`src-go/qa/rules/table.go` has a new `realClocks` row for `internal/rclonetail` `NewEnv`, with
its reason. The `"NewEnv"` function name now appears three times, so goconst asked for a
constant, `newEnvFn`, which the wsi and wsimedia rows now use as well. Production behaviour
is unchanged.

## Verification

- `./scripts/qa-go-qa.bash` (full, both modules, which includes `go test -race`): every stage
  passed.
- `./scripts/qa-changed.bash`: passed.

## Review

A fresh Opus general-purpose review gave the verdict MERGE. It made one optional note:
`CLAUDE/GoProjectRules.md:534` still says "14 realClocks" rows. That sentence belongs to the
rule's dated red/green proof, so it was left as it is.
