# meta-deploy queue moved to a data file

Branch `fix/meta-deploy-list-data`.

## What changed

- `CLAUDE/Plan/meta-deploy.list` holds the queue: one entry per line, `#` comment lines
  (kept verbatim from F44, they describe the entry below), blank lines ignored.
- `CLAUDE/Plan/meta-deploy.bash` reads it. Validation kept: missing plan folder, folder with
  no executable triage/deploy/acceptance, missing playbook or plan script are FATAL before
  anything runs. Added: a malformed line (absolute path, `..`, inline `#`, words after a
  playbook/script entry, a `/` path that is neither kind) is FATAL naming the line number;
  a missing list file fails loudly through the loop redirect; empty queue is reported plainly.
  `--list` works.
- To fit the ratchet the three duplicated if/else PIPESTATUS blocks became
  `cmd | tee log || status="${PIPESTATUS[0]}"` and the playbook/script/folder checks share one
  FATAL message ("nothing runnable at ..."). Record tightened: 185/0/188 -> 163/0/183 via
  `qa-bash-limits.bash --update`. The allowlist entry stays (assoc array remains).
- `scripts/test-meta-deploy.bash`: 14 cases (comments/blank ignored, triage words, empty queue,
  missing folder/script/playbook/list file, four malformed shapes, failing unit and failing
  acceptance exit 1 and are named, real list validates). Written red first (10 failed against
  the old script). It runs from `scripts/test-planlib.bash` because wiring a new gate into
  `qa-all.bash` would grow that ratcheted file.
- Docs now point at the list file: CLAUDE.md, CLAUDE/AgentNotes.md, CLAUDE/PlanWorkflow.md,
  CLAUDE/PlanScriptStandards.md, CLAUDE/Plan/CLAUDE.md. `.claude/hooks-daemon.yaml`
  `extra_root_files` gains `meta-deploy.list`.

## Follow-ups for the owner

- The plan-root file allowlist in `.claude/hooks-daemon.yaml` changed: restart the daemon
  (hooks-daemon skill, `restart`) for the new file to be accepted at the plan root.
- Pre-existing quirk, untouched: `--list` prints a script entry as its basename only
  (`acceptance.bash`), without the plan folder.

## Checks run

bash -n and shellcheck (clean for the new/changed scripts; test-planlib.bash carries
pre-existing warnings), `scripts/test-meta-deploy.bash`, `scripts/test-planlib.bash`,
`scripts/qa-bash-limits.bash`, `scripts/qa-docs.bash`. meta-deploy.bash was run only with
`--list` (and, in a scratch fixture tree, with throwaway scripts). qa-all.bash not run.
