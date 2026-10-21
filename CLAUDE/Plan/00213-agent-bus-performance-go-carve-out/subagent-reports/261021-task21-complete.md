# Task 2.1 complete

The Go `pingbus status-line` is finished. It is not cut over: that is Task 2.3.

- `src-go/internal/pingbus/statusline/cache.go` is the bus-segment cache (SPEC S32 to S36,
  plus the racy rule). `statusline.go` and `settings.go` are wired onto `world` and `pyjson`.
  `cmd/pingbus-status-line/main.go` has D3 and D4.
- Golden: 104/104 in-process, and 104/104 with `-impl=<built binary>`.
- `golangci-lint` and the project rules driver are clean over `internal/pingbus/...` and the
  command. `go test -race -shuffle=on` is green. Module coverage (`-coverpkg=./...`) is 92.4%.
- `scripts/go-binary-suites.tsv` gained `pingbus-status-line qa-go`, which `qa/changed`'s real-tree
  test requires of every command. `./scripts/qa-changed.bash` passes.
- One `.golangci.yml` exclusion: gosec G302/G304/G306/G703 for the statusline tests only.
- The departures and the racy rule are in SPEC section 9 and the journal.
- For review: the oracle's worlds are always fresh, so the oracle runs the uncached path. The
  cache's hits, misses and expiry are proved by `cache_test.go` with an injected clock.
