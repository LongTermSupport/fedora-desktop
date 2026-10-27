# reclaim 2.0.3: podman storage summarised, dangling-only prunes by default (branch `fix/00192-reclaim-summary-and-safe-prune`)

## Cause (host run of 2.0.2)

- The partial Podman section (E7a) printed the raw `podman images`, `podman ps --all --size`
  and `podman volume ls`: several hundred lines on the host, which made the report unreadable.
- Option 6's partial path offered `podman image prune -af`. Its full path offered
  `podman system prune -af`, and so did docker's (`docker system prune -af`). Each of these
  removes every image that no container uses, tagged ones included. On a ccy host that is every
  per-project `localhost/claude-yolo:<project>` image not running at the moment, several GB
  each, rebuilt on that project's next launch. Option 10 (the safe sweep) ran the same prune.
  The bash did the same (`system prune -af`), so this is departure B16.

## Fix (SPEC E7a, E22, E22a, E27, B16; `src-go/internal/reclaim/podman.go`, `containers.go`)

- The three listings are read as `--format json` and summarised. Images: count, summed size
  (a shared layer is counted in each image, so the figure is labelled as a sum), the dangling
  (`<none>`) count and size, and the largest ten. Containers: count and the largest ten by
  writable layer. Volumes: count and the first ten by name, since sizing a volume opens its
  data. Each kind adds `and <n> more` when it has more than ten. A listing that fails, or whose
  output is not a JSON list, is marked `unavailable` and named on stderr. The report and
  option 6 print the same lines. The unreadable-volume naming is unchanged.
- Default prunes are dangling-only. In the full path: `podman system prune -f` and
  `docker system prune -f`. In the partial path: `podman image prune -f`, whose question gives
  the count and size of what it frees.
- Removing every unused image is a separate question that defaults to No. For podman, it names
  the tagged images it would remove (count and size), lists the claude-yolo project images
  among them, and counts the rest (`podman image prune -af`). For docker, it says the same
  thing in words (`docker image prune -af`). The safe sweep never asks this question and says
  so instead.
- Scenario table: the -af scenarios are now -f, and a new `containers-podman-all-unused-yes`
  scenario was added. `parity.py go` and `parity.py bash` both report 29 scenarios, 0
  unexplained (B16 is named where the bash differs).

## Proof

The tests came first: the changed tests in `podman_test.go` and `actions_test.go` failed (13)
before the code was written. `go test -race` passes, `internal/reclaim` coverage is 94.4%, and
golangci-lint reports 0 issues. The version is 2.0.3.

## Host side

The `changed-plays` comment in `meta-deploy.list` names `play-cli-tools.yml` for 2.0.3.
`play-podman.yml` is no longer named there because the host already ran it at `acfcb6814`.
After that play the host agent reruns H1's Podman section, expecting a summary of a few dozen
lines. It then runs option 6 and answers No to both questions, expecting nothing to run.
`podman images --format json` field names (`Names`, `Size`, `Containers`, `Dangling`, and
`Size.rwSize` for containers) are taken from podman's JSON. That podman's real output matches
them can only be confirmed on the host: figures of 0 would mean a field was not matched.
