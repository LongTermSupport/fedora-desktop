## 10:10 · action · —   — Task 4.1 green; the prune timer in play-podman.yml

Task 4.1 green, Tasks 4.2 and 4.3 written. `internal/engine/store.go` (the store lists from
podman's JSON, `image rm` unforced, `rm --force` only for an external container, docker
refused for the podman-only lists) and `internal/containerimages/prune.go` (P1-P8 in its
package doc) make the red tests pass: `go test -race` green, coverage 93.4% (containerimages)
and 99.7% (engine); `golangci-lint run` 0 issues. `cmd/container-images` is the thin main.
play-podman.yml now imports play-golang.yml, builds the command, installs
container-images-prune.service and .timer, enables the timer, and starts the service once on
the run that first installs it. Task 3.4's stale project tags are in the same rule set (P2).
The orphaned-volume step is left as Task 4.4: it deletes volume data as root in the user's
store and wants its own review. deploy.bash and acceptance.bash added; the existing
meta-deploy.list entry for this plan now runs triage, deploy, triage, acceptance.
