## 10:01 · action · —   — Task 4.1 red: prune tests before the code

Task 4.1 red: the tests are written before any source. `src-go/internal/engine/store_test.go`
(the store's lists read from podman's JSON, the removals, docker refused for the podman-only
lists) and `src-go/internal/containerimages/prune_test.go` (dry run removes nothing; `--yes`
removes old unused dangling images, logs each after it went, looks again until a pass removes
nothing, bounded; stale `claude-yolo:<project>` tags over 30 days, never `:base`/`:latest`;
buildah working containers over a day only while no ccy build lock is held; failed removal named,
not logged, exit 1; volumes listed never removed; never `system df`; usage). Both fail to
compile: `e.StoreImages undefined`, `undefined: App`.
