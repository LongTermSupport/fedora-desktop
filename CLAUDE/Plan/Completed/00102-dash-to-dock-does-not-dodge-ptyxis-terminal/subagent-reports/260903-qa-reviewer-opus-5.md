## QA Review — CCY 3.48.0 / container 2.34: browser mode is an explicit choice

**Verdict**: FIX-BEFORE-MERGE

Scope note: the working tree was clean when I finished — the change was committed as `e42d2fe` and pushed to `origin/F44` mid-review. I reviewed that commit (identical content, plus a `case` guard on `$REAL` added after my first read). Untracked `CLAUDE/Plan/00102-…` and modified `CLAUDE/Plan/README.md` in the tree are concurrent unrelated work by another agent.

### Should fix

**1. Nothing verified what a caller-supplied `--headed` does to the wrappers — and the reference the skill mandates recommends adding one.** `<repo>/files/var/local/claude-yolo/Dockerfile:261,267` put the wrapper's own flags *before* `"$@"`:

    exec $REAL --namespace headless --headed false "$@"

The version-matched reference the skill orders the agent to read (`agent-browser-headless skills get core --full`) teaches `--headed` in nine places, including as the prescribed fix for a black WebGPU screenshot — i.e. exactly while doing unattended headless screenshot work:

> add `--headed` (needs a logged-in desktop on Windows; on Linux agent-browser starts a private virtual display automatically…)

If the caller's flag wins, `agent-browser-headless --headed …` opens a real window (CCY forwards `WAYLAND_DISPLAY=wayland-0` / `DISPLAY=:0`, so no Xvfb fallback) **and** binds the `headless` namespace's daemon to headed state — the precise "first mode wins" failure `Dockerfile:243-249` documents as the reason namespaces exist.

I could not settle it. I measured repeated `--config` on this binary and it is **first-wins**, which suggests the wrapper holds:

    B=/usr/local/lib/node_modules/agent-browser/bin/agent-browser-linux-x64
    "$B" --config <missing> --config <valid>  --version   # rc=1  "config file not found"
    "$B" --config <valid>  --config <missing> --version   # rc=0

But `--config` is a string flag and `--headed` is an optional-value boolean parsed elsewhere; `--headed notabool` is not validated at all, and `--debug` prints nothing for a non-launching subcommand — so proving it needs an actual browser launch, which I would not do during a review. The build-time checks (`--version`, `Dockerfile:308-310`) and the acceptance runs all used clean argv, so none of them covers this path. Run the probe; if the caller wins, make the wrapper authoritative (reject a caller-supplied `--headed`/`--namespace`/`--config` with a clear error, or append the wrapper's flags last), and record the answer in the Dockerfile comment, which currently asserts only that the daemon does not re-invoke by PATH name.

**2. The reference the skill mandates writes the blocked name 647 times.** `<repo>/files/opt/claude-yolo/skills/browsing/SKILL.md:84-90` says "Read that before running anything" and points at `agent-browser-headless skills get core --full`. That document is 2907 lines, with 647 lines invoking the bare `agent-browser` command (`agent-browser open <url>`, `agent-browser snapshot -i`, …). Every one of them now exits 2. Neither SKILL.md, `CCY-GUIDE.txt:102-103` nor the stub (`Dockerfile:294`) tells the agent to substitute its chosen command name for `agent-browser` in the reference's examples. One sentence in all three closes it.

**3. Line 38 of that same reference is the upstream global-npm setup one-liner.** It is the same global npm command as `Dockerfile:159`, which is what created `/usr/local/bin/agent-browser` in the first place — so an agent that hits the stub, reads the reference and follows that line silently recreates the symlink and un-blocks the bare name for the rest of the session. The stub says "not available in CCY" rather than "not installed", which helps, but it should say outright that re-adding the npm package is not the fix.

**4. The new docs section is filed under `ccy.env`, where it is not configurable.** `<repo>/docs/ccy.md:609` adds `#### Browser mode is always an explicit choice` inside `### 2. ccy.env — per-project environment` (`docs/ccy.md:538`), a list of environment variables a project sets. Browser mode is not one — it is fixed in the image and cannot be set from `ccy.env`. `## Working Inside a CCY Container` (`docs/ccy.md:918`) is the natural home.

**5. No "BASE IMAGE CHANGES" notice for 2.34.** `<repo>/files/opt/claude-yolo/ccy-startup-info.txt:44-49` carries one for image 2.33, and `<repo>/docs/ccy-changelog.md:53-55` states that entry exists specifically "so a project agent sees this on its first session in the rebuilt image". Removing `agent-browser` from PATH is the same class of break — any project whose scripts, hooks or `CLAUDE.md` name it now gets exit 2 — and it gets no such notice. The 3.48.0 changelog entry (`docs/ccy-changelog.md:20-40`) likewise has no "Action for any project that used them" paragraph, unlike 3.47.0 at lines 51-55.

**6. `CUSTOM-DOCKERFILES.txt` was not updated and is baked into the image.** `<repo>/files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt:55` still reads "Browser: agent-browser CLI + Chromium (Wayland/X11 support)" and `:172` lists `agent-browser` among "Tools already in base". It is `COPY`d at `Dockerfile:325`, sits beside `CCY-GUIDE.txt` which *was* updated, and is the one document a custom-Dockerfile author reads — so it is also the right place to warn that re-adding the agent-browser npm package to a project image undoes the block.

**7. Four shell scripts now live outside every QA gate.** `Dockerfile:256-297` generates them as `printf '%s\n'` string literals, so `qa-bash.bash` never sees them — `qa-all.bash` reported "bash: 187 files OK" and none of the four can be among those, because `qa-discovery.bash` walks tracked files. `sh -n` at build time is a parse check, not shellcheck. The three exec wrappers genuinely need build-time `$REAL`, so they have a reason to be generated. **The stub does not** — `Dockerfile:278-296` is 17 lines of entirely static `/bin/sh` with no substitution at all. Make it a real file under `files/var/local/claude-yolo/`, deployed into the build context by `play-claude-yolo.yml` and `COPY`d, exactly as `entrypoint.sh` (`Dockerfile:317`), `skills/browsing/` (339), `optional/` (349) and the PHPantom plugin (402) already are — and it becomes reviewable and shellcheck'd.

### Design question

The minimal fix for the reported harm was dropping the `"headed": true` key from `config.json` — which this change also does (`Dockerfile:162-168`) — leaving `agent-browser --headed` for the watch case. The three-name design additionally buys a stub, symlink surgery, a 62-line `RUN` block, four ungated scripts, and permanent divergence from 647 upstream examples. Forced deliberation may well be worth that; it is worth being explicit that forced deliberation is the *only* thing the extra machinery buys, since the config-key removal alone already stops background research opening windows.

### Nits

- `<repo>/README.md:187` still presents `agent-browser` in backticks as CCY's built-in browser command.
- `<repo>/files/var/local/claude-yolo/Dockerfile:180` — "Applies to both engines; each daemon reads it". Three daemons now.
- `<repo>/files/opt/claude-yolo/ccy-startup-info.txt:25` is 121 characters where every other line in the file is <= 95. It wraps at ordinary terminal widths and breaks the column alignment of the block it heads.
- `<repo>/files/opt/claude-yolo/skills/browsing/SKILL.md` states the same fallback twice — lines 20-21 and again at 56-57.
- `SKILL.md:79` and `docs/ccy.md:607` tell the agent to "run `session list`" with no command prefix; the old text (`agent-browser session list`) was copy-pasteable.

### Checked and clean

- **Version bumps**: `Dockerfile:36` LABEL `2.34` == `claude-yolo:67` `REQUIRED_CONTAINER_VERSION="2.34"`; `claude-yolo:17` `CCY_VERSION="3.48.0"` with a rewritten comment; changelog 3.48.0 names container 2.34. `validate_container_version` (`lib/common.bash:543-563`) keys on the label plus the Dockerfile md5, so the bump forces the one-time rebuild. No stale `2.33` reference outside the deliberate historical ones.
- **Old names**: no `agent-browser-lite` or bare-command *usage* survives outside `CLAUDE/Plan/` and the historical changelog entries, and no `--headed` flag guidance survives anywhere in `docs/`, `files/`, `playbooks/` or `README.md`.
- **Fail-fast**: the RUN opens `set -eu`, chains everything with `&&`, and the bare-name assertion (`Dockerfile:311`) is probe-then-fail with the result explicitly checked. No `failed_when: false` / `ignore_errors:` introduced. The directory creation at line 255 is correct — the first `COPY` into `/opt/claude-yolo` is line 321.
- **Stub behaviour** (measured in-container): all four blocked names — `agent-browser`, `agent-browser-lite`, `agent-browser-lite-headed`, `agentbrowser` — print the matrix and exit 2, message on stderr, stdout empty. Correct stderr hygiene.
- **Wrapper argv**: `--headed` takes an optional `true`/`false` value per the CLI's own help, and `agent-browser-headed skills list` returns rc 0 with clean stdout and empty stderr, so the wrapper flags do not swallow the subcommand.
- **Env-var risk**: CLI flags are precedence 4, "Override everything", above environment variables at 3 — so a project setting `AGENT_BROWSER_HEADED` in `ccy.env` cannot un-headless the headless wrapper.
- **Build-time config check**: `--version` *does* load and validate `--config` (a missing path exits 1), so `agent-browser-lite-headless --version` at `Dockerfile:310` genuinely proves `lightpanda.json` parses — that check is not the empty one it looks like.
- **Placement in the IaC graph**: the change lives entirely in the play that owns CCY (`play-claude-yolo.yml`) and the image it builds. No new play, no misplaced concern, no ordering change.
- **Naming**: no "hygiene"/"management"/"support" jargon; the mode is in every command name; `agent-browser-mode-required` names the condition it enforces. No `: -x` pattern in any task `name:`.
- **Public-repo safety**: the only identifier-shaped strings in the diff are `user@example.com` and `https://example.com`, both sanctioned by `CLAUDE/ExampleValues.md`.
- **Plan Commit Rule**: no plan folder tracks this work, so nothing is left dangling by the commit.

### Mechanical gates

- `scripts/qa-all.bash`: **PASS** — "QA passed: 647 files checked" (shellcheck 137 advisory findings, pre-existing).
- `hooks-daemon plan-qa --sweep`: 1 block + 2 advise, **all pre-existing and unrelated** — the block is `00102-dash-to-dock-…` missing a README index row (concurrent work by another agent).
- `hooks-daemon docs-qa --sweep`: 39 advisories, 0 block, **none in the changed files**.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: **OK** (exit 0).
- Conditional gates: the diff touches no `helpers/`, `tests/helpers/`, `extensions/**/metadata.json` or extension JS, so `qa-helper-tests.bash`, `check_extension_compat` and ESLint are **not triggered** by this change — `qa-all.bash` ran helper-tests (203) and extension-compat regardless, both green.
