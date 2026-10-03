## QA Review: Plan 00150 (d94a14cd..cf6bc90f, imgpaste and play-cli-tools.yml)

**Verdict**: FIX-BEFORE-MERGE

Nothing blocks the merge. Three defects need fixing first: two refusal checks can pass on a broken tool, a fail-fast hole lets `imgpaste` exit 0 with a corrupt block, and an acceptance criterion has no test.

### Should fix

1. **The refusal checks don't check why the command refused.** `CLAUDE/Plan/00150-image-paste-cli/acceptance.bash:111-130`
   Both checks pass whenever the command exits non-zero with nothing on stdout. Suppose `magick` fails to write WebP, or the pixel limit trips. The "random noise is refused" check still prints PASS, but the over-budget ladder never ran. A blind failure looks the same as a correct refusal.
   - Fix: also assert that `refused-noise.err` contains `no setting fits`.
   - Fix: also assert that `refused-text.err` contains `not an image`.

2. **The oversized-input rejection is never tested, but Task 2.4 is ticked.** `PLAN.md:62`, `PLAN.md:71`, `imgpaste:39,43`
   - Task 2.4 says acceptance "tests the rejection paths", and success criterion line 71 promises that oversized inputs fail fast.
   - `acceptance.bash` has no case over `MAX_INPUT_BYTES` and none over `MAX_INPUT_PIXELS`. The noise image is 2000x2000, which is 4M pixels against an 80M limit.
   - Fix: add a pixel-limit case. `magick -size 9000x9000 xc:white` gives 81M pixels, which trips the limit before any encoding. Assert on the stderr text.
   - Fix: add a byte-limit case, or state in the COVERAGE output that it is not covered.

3. **`render_block` ignores errors because it runs inside `$(…)`.** `files/home/.local/bin/imgpaste:49-60,67`
   - Bash turns off `set -e` inside command substitution unless `inherit_errexit` is set. I confirmed this: `bash -c 'set -euo pipefail; f(){ false; echo after; }; x=$(f); echo "status ok, x=$x"'` printed `status ok, x=after`.
   - So if `sha256sum`, `base64` or `magick identify` fails inside `render_block`, imgpaste still prints a block and exits 0.
   - Example: a failed `base64` gives an empty payload, which easily fits the budget. Only the receiver's `sha256sum -c` would catch it. That breaks the fail-fast rule, which applies to the tool itself, not just to whoever uses its output.
   - Fix: add `shopt -s inherit_errexit` after `set -euo pipefail` at line 9.

4. **The plan's README index row is out of date.** `CLAUDE/Plan/README.md:39`
   It still says "next is the dogfood decode (Task 1.4)". Phase 2 is now mostly done. Update the row in the same follow-up commit.

### Nits

- `acceptance.bash` never calls `plan_finish`, so the run-log path isn't printed. Plan 00137, 00139 and 00109 all call it, though several older plans don't either.
- The regex `r[w]` at `play-cli-tools.yml:52` is just `rw` written in an odd way.
- The "Delivery & Milestones" section at `PLAN.md:81` doesn't record cf6bc90f.
- In the play, only the imgpaste tasks are tagged. With `--tags imgpaste`, the task that creates `~/.local/bin` (line 24) is skipped. The copy at line 58 still creates the file, so this only matters on a fresh host where the directory doesn't exist yet.

### Checked and clean

- **IaC placement:** The owner decided on one aggregating play, and it's written down in `CLAUDE/AnsibleStyle.md:46-61`. The play is optional, so it isn't listed in `playbook-main.yml`, which is correct. `hosts: desktop` and `become: true` match the other general-scope plays (54 of 54 use `hosts: desktop`). `root_dir` and `scope` follow the house style.
- **Probe-then-fail:** I ran `magick -list format` here. IM7 prints `     WEBP* rw+   WebP Image Format…` in the Format/Mode layout, and the assert regex matches it.
- **`magick --` and `identify -- file[0]`:** both work on IM7 against the fixture. I checked with `info:` output only, so nothing was written.
- **Stderr hygiene:** stdout carries only the block, and `--help` goes to stdout. Usage errors, `die` messages and the summary line go to stderr. The summary is written as a `#` comment, so pasting it by accident is harmless.
- **Plan scripts:** they follow R1 (the bootstrap), R2 (`plan_require_host`), R3 (sudo is primed before the run log starts), R4, R6, R7 (deploy mode with a top-level `plan_deploy_leg`) and R8 (no confirmation prompt). The header says what the run changes, and the files are executable.
- **Production path:** acceptance runs the deployed `~/.local/bin/imgpaste`, uses `cmp` to confirm it matches the repo copy, and checks that PATH resolves to it. It runs the block with bash in a work directory, so the cwd-relative decode path is exercised, and it prints a COVERAGE line.
- **Public-repo safety:** the screenshot fixture shows only chat text, with no user, host or path. I grepped the diff for home paths, emails and private IPs and found nothing. `Owner: joseph` is the standard scaffold value (123 plans use it).
- **Version bumps:** none needed, because nothing under `files/var/local/claude-yolo/` changed.
- **Docs:** `docs/playbooks.md:772` documents the play, and the anchor matches the AnsibleStyle heading. No stale `imgpaste-proto` references remain outside the append-only JOURNAL.

### Mechanical gates

- **qa-all.bash:** not run, as instructed; the coordinator owns it. I ran `shellcheck -x` on `imgpaste`, `deploy.bash` and `acceptance.bash` instead: exit 0, no findings.
- **plan-qa --sweep:** 0 blocking, 11 advisory findings, none of them for Plan 00150. It exits 1 even with no blocking findings.
- **syntax-check:** `ansible-playbook --syntax-check play-cli-tools.yml` passed.
- **Not triggered:** `qa-helper-tests.bash`, the extension compatibility check and ESLint. The Plan 00150 commits don't touch `helpers/` or `extensions/`.
- **Host-only:** the end-to-end run of imgpaste still needs the host. The container has no `file` binary, and I installed nothing.

Files referenced:
- /workspace/files/home/.local/bin/imgpaste
- /workspace/playbooks/imports/optional/common/play-cli-tools.yml
- /workspace/CLAUDE/Plan/00150-image-paste-cli/acceptance.bash
- /workspace/CLAUDE/Plan/00150-image-paste-cli/PLAN.md
- /workspace/CLAUDE/Plan/README.md