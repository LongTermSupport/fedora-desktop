# Review: container version gate extension (Task 1.1, `fix/container-version-gate` @ 443c29b7)

Adversarial pre-merge review of `git diff F44...origin/fix/container-version-gate`. Read-only;
no qa-all. The branch was checked out into a scratch worktree, the hook test and the bash
limits gate were run there, and the real hook was probed in throwaway repositories.

## Verdict: FIX-FIRST

One blocking finding (B1), four non-blocking (N1–N4), two informational (I1–I2).

## What was verified

- `scripts/test-ccy-container-version-hook.bash`: 43 passed, 0 failed on the branch.
- `scripts/qa-bash-limits.bash`: `BASH-LIMITS-OK`; the allowlist ratchet for
  `scripts/git-hooks/pre-commit` moves 312 → 311 statements, lines and functions unchanged.
- Drift test does fail when a new copy task the gate misses is added: inserting a copy of
  `README.md` to `/opt/claude-yolo/optional/README.md` into the play (under the Dockerfile's
  directory `COPY optional/`) produced `FAIL: a change to README.md without a container bump is rejected → '0 no'`. A dest not under any Dockerfile COPY source (e.g. `docs/README.md`,
  since the Dockerfile copies the two docs files by name) is correctly ignored.
- `helpers/agent_bus/**` does land in the image: `helpers/pingbus/bundle.py` has
  `PACKAGES = ("helpers/pingbus", "helpers/agent_bus")`, the play's "Build the pingbus Zipapp"
  task writes `/opt/claude-yolo/optional/agent-bus/pingbus`, and `COPY optional/` bakes it in.
  Gating it is not churn.
- Every play task writing under `/opt/claude-yolo/` maps to a gated repo path: Dockerfile,
  entrypoint.sh, the two agent-browser stubs, supervisor-plugins/, plugins/ (all
  `files/var/local/claude-yolo/`); ccy-startup-info.txt, docs/*, skills/browsing/SKILL.md,
  optional/child-claude/*, optional/agent-bus/{settings.json, plugin/pingbus/\*} (all
  `files/opt/claude-yolo/`); the zipapp (helpers/pingbus, helpers/agent_bus). The
  `custom-dockerfiles/` templates are in the context but under no COPY source, so correctly
  not gated. No `ADD`, `template`, `synchronize` or `--build-arg` from repo content was found.
- Rename of an image file is caught: `git mv a.py c.py` under supervisor-plugins → rc=1 with
  the bump message (the per-path `git diff --cached HEAD -- <new path>` cannot pair the
  rename, so the file shows as a full addition).
- ContainerRules.md and QA.md rows describe the new behaviour accurately.

## B1 (blocking): a staged deletion of an image file passes without a bump

`STAGED_FILE_LIST` is built with `--diff-filter=ACMRT` (hook line ~21), so a deleted file is
never in `CCY_IMAGE_STAGED`. Probed on the branch's hook in a throwaway repo:

```
DELETE helpers/pingbus/x.py        -> rc=0 bump-msgs=0
DELETE supervisor plugin b.py      -> rc=0 bump-msgs=0
```

Deleting a supervisor plugin, a pingbus/agent_bus module, or a skill leaves the running image
carrying the deleted file, which is exactly the stale-copy case the gate exists for, and the
branch's own comment says the test "checks these lists against every file play-claude-yolo.yml
puts into the image". The gap is pre-existing, but this branch widens the gate's claim to
trees where deletion is a normal event (Plan 00202 itself dropped the `/team` commands).
Fix: build the image gate's candidate list with `D` included (e.g. a second `mapfile` with
`--diff-filter=ACDMRT` used only by the container gate), and add a deletion case to
`test-ccy-container-version-hook.bash`. The content check already handles it: a deletion is
all `-` lines.

## N1: `#`-prefixed changes are discarded as comments, but the gate now covers markdown

The content check drops `^[+-][[:space:]]*(#|$)` lines. In `.py`/`.sh` that is a comment; in
`SKILL.md` (three of them are now gated), `CCY-GUIDE.txt` and `CUSTOM-DOCKERFILES.txt` a `#`
line is a heading, and in any script the shebang. Probed: a heading-only change to a markdown
file under `helpers/pingbus/` and a shebang-only change to a `.py` both pass with rc=0.
Suggest applying the comment filter only to files whose extension or shebang marks them as
shell/Python, or dropping it for `.md`/`.txt`. Pre-existing logic; not blocking.

## N2: false positives under `optional/agent-bus/` (host kit, not image)

The Dockerfile `COPY optional/` makes the gate cover all of
`files/opt/claude-yolo/optional/agent-bus/`, but the play copies only `settings.json` and the
three `plugin/pingbus/` files into the build context. `README.docker`, `README.host`,
`README.lxc`, `README.podman`, `README.vm` and `agent-bus-claude` are deployed by
`play-agent-bus.yml` / `agent-bus-install`, never into the image, yet a commit editing them
now needs a container bump. The drift test proves gate ⊇ image, not gate ⊆ image, so this is
invisible to it. Acceptable churn given the directory COPY (the Dockerfile would take them if
the play ever copied them), but worth a note in ContainerRules.md or a narrower rule. Also
`helpers/pingbus/bundle.py` is `EXCLUDE`d from the zipapp yet gated; defensible, since it
determines the zipapp's bytes.

## N3: the drift test's "teach me" guard only sees tasks with a `dest`

The `KNOWN`-module check fires only when a task's args carry `dest` under
`/opt/claude-yolo/`. A `command`/`shell` task writing into the context by argv (other than the
`helpers.pingbus.bundle` special case), an `ansible.builtin.file` `state: link` into the
context, or a build step reached through `include_tasks`/`import_tasks` (the play uses
`build-go-command.yml`) would be silent. Nothing like that exists today; note it in the test's
comment so the next zipapp-like step is added deliberately.

## N4: mode-only change passes

`chmod +x` alone on an image file → rc=0. Harmless today: the play sets `mode:` on every copy
and the zipapp fixes entry modes, so repo mode never reaches the image. Recorded for
completeness.

## I1: in-flight branches touching gated trees

| Branch                              | File                           | Bumps label | Last commit |
| ----------------------------------- | ------------------------------ | ----------- | ----------- |
| `origin/go-port/agent-bus-install`  | `helpers/agent_bus/declare.py` | no          | 2026-10-14  |
| `origin/wf-f0f65b6e-87f-3-061c2adc` | `helpers/pingbus/protocol.py`  | no          | 2026-10-06  |

Neither is merged into F44. Both must bump `LABEL claude-yolo-version` and
`REQUIRED_CONTAINER_VERSION` before merge. Note the gate will not force this by itself: a
clean `git merge` runs `pre-merge-commit`, not `pre-commit`, and `core.hooksPath` ships no
`pre-merge-commit`, so merging either branch after this gate lands produces a stale image
silently. The owner has to check, or the branch has to make one post-gate commit touching the
file.

## I2: both roots get every COPY source

`CCY_IMAGE_PATHS` crosses every Dockerfile COPY source with both context roots, so paths like
`files/opt/claude-yolo/entrypoint.sh` are gated though they do not exist. Harmless.

## Files

- `/workspace/scripts/git-hooks/pre-commit` (branch): `--diff-filter=ACMRT` at the staged list;
  `CCY_CONTEXT_ROOTS` / `CCY_IMAGE_PATHS` in the container gate.
- `/workspace/scripts/test-ccy-container-version-hook.bash` (branch): drift test, `KNOWN` set.
- `/workspace/helpers/pingbus/bundle.py`: `PACKAGES`, `EXCLUDE`.
- `/workspace/playbooks/imports/play-claude-yolo.yml`: build-context tasks, zipapp build.
