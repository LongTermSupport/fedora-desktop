# Plan 00232: build handoff after Tasks 3.1 and 3.2

Branch `feat/00232-netpolicy-build`. Done: the design settled from the rerun (DESIGN.md
section 10; F31 to F40), and the Go command `ccy-network-policy` (`src-go/cmd/ccy-network-policy`,
`src-go/internal/netpolicy`, N1 to N9 in its package doc, coverage 92.8%, lint clean, row in
`scripts/go-binary-suites.tsv`). Left: Tasks 3.3 and 3.4. Nothing is wired yet, so nothing
changes on a host until 3.3 lands.

## What the command expects of the launcher (Task 3.3)

- **The one call**: `lib/agent-choice.bash` `ccy_agent_container` ends with `container_cmd "$@"`.
  Replace that line with
  `ccy-network-policy launch --name "$CONTAINER_NAME" --image "$IMAGE_NAME" -- "$CONTAINER_ENGINE" "$@"`.
  One statement for one, so neither file grows (the launcher is on the bash ratchet:
  `scripts/bash-limits.allowlist`, claude-yolo 2775/12/2707). The argv is the launcher's own
  `run $DOCKER_FLAGS --rm --name ... $NETWORK_FLAG ... "$IMAGE_NAME" claude ...` (claude-yolo
  around line 3880); `launch` turns `run` into create, init, guard, apply, verify, then
  `start --attach --interactive`, and returns the container's exit status (75 included). It adds
  `-v $XDG_RUNTIME_DIR/ccy/network-policy/<name>/context:/run/ccy/network-policy:ro[,$CCY_MOUNT_RELABEL]`
  and the `CCY NETWORK POLICY` block to `claude`'s `--append-system-prompt` (merged into the
  startup one when the launcher passes it). Secrets reach `podman create` by environment as now.
  Exit 127 (not installed) should get a clear message like `lib/token-usable.bash` gives, if the
  file's limits allow.
- **--connect / --disconnect**: claude-yolo around line 1163 calls `connect_to_network ...` then
  `exit $?`. Make each `connect_to_network "$CONNECT_NETWORK" "_yolo" "ccy" && ccy-network-policy refresh`
  (and the same for disconnect), keeping the statement count.
- **The play** (`playbooks/imports/play-claude-yolo.yml`): add `nftables` to the package list at
  "Install Claude Code Dependencies (System Level)" (about line 275), and a build task for
  `ccy-network-policy` to `~/.local/bin` like `ccy-token-usable` (about line 641).
- **Version**: `CCY_VERSION` minor bump (3.100.36 now; container version unchanged unless the
  Dockerfile or entrypoint changes), `docs/ccy-changelog.md`, `docs/ccy.md` (networking and
  security: the default, the host file from `ccy-network-policy where`, the keywords, Docker and
  no-nft warn and run unprotected).
- Check the launcher's differential harness (`internal/ccylaunch/difftest`): its fakes record
  podman argv, so a launch scenario's golden will now show `ccy-network-policy` (the fake PATH
  may need a fake of it). Run `go test ./internal/ccylaunch/...`.

## Task 3.4

- `deploy.bash` in the plan folder: the play, then two throwaway headless seated sessions in a
  test team (both `ccy --headless --teams <team>`, a long prompt so they stay up), printing their
  names; built on `_planlib.inc.bash` (PlanScriptStandards.md).
- `git mv acceptance-policy.bash acceptance.bash`, and point its header at deploy.bash.
- `meta-deploy.list`: the plan entry with an exact description (AgentNotes.md: what the play
  changes, the two throwaway sessions, the one probe message each way, the throwaway control
  container), above or below `changed-plays` as its comment there says.
- `./scripts/qa-changed.bash`, then the `qa-reviewer` agent.

## Watch

- `refresh` treats a container whose inspect says "no such" as gone and removes its state; any
  other failure is exit 1 naming the session.
- The JSON read-back (`nft -j list table`) forms are from libnftables-json and only proven
  against fixtures here: the host acceptance's `held` check is the first real read-back.
- The bus address is opened on every port (DESIGN.md section 10, known exposure).
