# Integration Constraints — Adding a Second Browser Engine to CCY

**Purpose**: establish, from the repo itself, exactly how a second (lightweight,
JS-executing) browser engine would have to be wired into CCY alongside the
existing Chromium/`agent-browser` stack — every file that changes, every
version-bump rule that applies, every QA gate that applies, and every
environmental constraint (arch, glibc, root, no-systemd, image-size budget)
that would disqualify a candidate before evaluation even starts.

No verdict on which engine to adopt is given here — this is the constraint
surface, established only from source in this repo
(`/workspace/files/var/local/claude-yolo/*`, `/workspace/playbooks/imports/play-claude-yolo.yml`,
`/workspace/CLAUDE/ContainerRules.md`, `/workspace/docs/containerization.md`),
plus the sibling skill docs. Every claim below cites the exact file/line it
came from. Dated 2026-08-12.

---

## 1. The container's hard environment facts

| Fact                         | Value                                                                                                                          | Source                                                                                                                                                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Base image                   | `node:lts-slim` (floating LTS tag, not pinned to a major)                                                                      | `Dockerfile:24`                                                                                                                                                                                                       |
| OS / libc                    | Debian 12 "bookworm", **glibc 2.36**                                                                                           | `Dockerfile:4-6` comment: "the pre-built PHPantom LSP binary requires GLIBC 2.39 … our runtime base image is Debian 12 bookworm (GLIBC 2.36)"                                                                         |
| Arch                         | **amd64 only** — no multi-arch build in this Dockerfile                                                                        | `Dockerfile:110` hardcodes `yq_linux_amd64`; the PHPantom build stage targets only `x86_64-unknown-linux-musl` (`Dockerfile:12,16`)                                                                                   |
| Runtime user                 | **root** inside the container (rootless Podman maps this to the host user)                                                     | `Dockerfile:211-213` "Note: USER directive is NOT set here — we use --user flag at runtime"; `ContainerRules.md` / `docs/containerization.md`: "Running as root inside the container is safe [under rootless podman]" |
| Container engine             | Podman (rootless) by default, Docker (rootful) as an alternate `--engine`                                                      | `claude-yolo:143` `--engine ENGINE` flag; `ContainerEngines.md`                                                                                                                                                       |
| Init system                  | **None** — `tini` is PID 1 purely for signal/zombie reaping, not a service manager; no systemd                                 | `Dockerfile:214-215` `ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]`                                                                                                                             |
| GUI transport                | Host **Wayland socket only** is bind-mounted, read-only, single socket (not the whole `XDG_RUNTIME_DIR`) — X11 fallback exists | `claude-yolo:2706-2732` (`GUI_MOUNTS`), comment: "mount ONLY the Wayland display socket (read-only) … bind-mounting all of it would hand a --dangerously-skip-permissions agent the host session bus and keyring"     |
| Network at container runtime | Whatever the launch config grants (no special sandboxing beyond the container boundary itself)                                 | inferred from `claude-yolo` flag set (`--network`, `--no-network`)                                                                                                                                                    |

**Disqualifying implications for a candidate engine**, straight from these facts:

- **Anything requiring a glibc newer than 2.36** cannot run as a prebuilt glibc
  binary in the runtime stage — it would need a **musl static build** (the
  pattern this repo already uses for PHPantom LSP, see §3) or a `rust:slim`/
  similar builder stage compiling from source.
- **Anything shipping only non-amd64 binaries**, or requiring architecture
  detection/multi-arch logic, is incompatible as-is — nothing in this
  Dockerfile or in `claude-yolo` handles `$(dpkg --print-architecture)` beyond
  the one inline call building the GitHub CLI apt source list (`Dockerfile:103`);
  everything else is hardcoded `amd64`.
- **Anything that requires systemd, a running daemon under init, or dbus
  session services beyond what's already mounted** does not fit — there is no
  service manager, and the Wayland mount is deliberately scoped to just the
  compositor socket (no D-Bus, no PipeWire, no keyring).
- **Anything requiring non-root execution as a hard requirement** is actually
  fine — the container already runs as root by design — but anything requiring
  **privilege dropping / setuid sandboxes** (as Chromium's own sandbox would,
  absent `--no-sandbox`) needs the same `--no-sandbox`-style workaround already
  applied to Chromium (`Dockerfile:161-164`).

---

## 2. Multi-stage Dockerfile layout (exact structure a new engine must fit into)

```
FROM rust:slim AS phpantom-builder      # (1) compiler toolchain stage, musl target
    → produces a static binary, copied into `full` later

FROM node:lts-slim AS base              # (2) the stage EVERYTHING else builds on
    - apt: curl/wget/jq/ripgrep/... (general CLI tools)
    - apt: poppler-utils/imagemagick/ghostscript/... (doc/image tools)
    - GitHub CLI (its own apt repo)
    - yq (direct binary download, amd64-only)
    - ansible (pipx) + semgrep (pipx) + uv (installer script)
    - Claude Code (npm -g)
    - apt: 17 Chromium runtime libs               ← Chromium's OS-level deps
    - agent-browser (npm -g) + `agent-browser install`  ← downloads Chromium itself
    - /root/.agent-browser/config.json             ← headed/Wayland browser args
    - agentbrowser→agent-browser typo stub
    - ctrl+z SIGSTOP patch (Node script, patches Claude Code's cli.js/binary)
    - entrypoint.sh, startup docs, skills/browsing/ (copied in)
    ENTRYPOINT tini → entrypoint.sh

FROM base AS full                        # (3) = base + pre-bundled LSPs
    - npm -g: typescript-language-server, pyright, bash-language-server, intelephense
    - COPY --from=phpantom-builder the static PHPantom binary
    - COPY the PHPantom Claude Code plugin
    LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"   ← LAST layer, deliberately
```

- `base` is a **published, documented extension point**: `docs/containerization.md`
  states projects needing a leaner image can `FROM claude-yolo:base` (line
  ~224 area / Dockerfile comment `Dockerfile:220-225`). Whatever a new browser
  engine needs at the OS/npm level should almost certainly land in `base`
  (next to the existing Chromium apt layer), not only in `full`, unless it is
  explicitly meant to be an LSP-tier opt-in extra.
- The `claude-yolo-dockerfile-hash` LABEL is deliberately the **very last
  layer of the `full` stage** (PERF-02, `Dockerfile:38-40,244-248`) so that a
  one-line Dockerfile edit doesn't bust every apt/npm layer above it in the
  cache. **This optimisation only protects against edits below the changed
  point** — inserting a **new** apt/npm layer for a second browser engine
  necessarily busts every layer *after* the insertion point on the next build,
  same as adding the Chromium layer originally did. Placing the new engine's
  install as late as possible (just before the hash LABEL) minimises this
  cache-bust cost on future unrelated edits.
- Precedent for **build-from-source with a compiler toolchain**: the
  `phpantom-builder` stage already does this (`rust:slim`, `musl-tools`,
  `cargo build --release --target x86_64-unknown-linux-musl`), and its
  artifact is copied into `full` with `COPY --from=phpantom-builder`. A
  candidate engine that must be compiled (Rust, Go, etc.) has a working
  template to copy; a candidate needing a **large/slow toolchain** (e.g. a
  full C++ build) pays that cost only in the builder stage — it does not
  bloat the runtime image, as long as only the compiled artifact is `COPY`'d
  forward and the builder stage is never referenced by `base`/`full`'s `FROM`
  chain.

---

## 3. Version-bump requirements — every value that must move together

Two **independent** version/hash pairs exist, both self-validating and both
capable of throwing a "developer forgot to bump" error that forces a rebuild
loop if mismatched. **Adding a browser engine touches only the Dockerfile
pair**, unless the `claude-yolo` wrapper script itself also needs new flags/
logic for the engine (e.g., a new CLI flag), in which case both pairs move.

### 3a. Dockerfile / container image pair (near-certainly required)

| Value                        | File             | Current value found |
| ---------------------------- | ---------------- | ------------------- |
| `LABEL claude-yolo-version`  | `Dockerfile:36`  | `"2.22"`            |
| `REQUIRED_CONTAINER_VERSION` | `claude-yolo:44` | `"2.22"`            |

**Both must be bumped to the identical new string in the same commit.**
Mechanism (`lib/common.bash:456-533`, `validate_container_version()`):

- Image build embeds `claude-yolo-version` and a computed
  `claude-yolo-dockerfile-hash` (md5sum of the Dockerfile, 16 hex chars) as
  OCI labels.
- On every `ccy` launch, `validate_container_version()` compares the running
  image's labels against `REQUIRED_CONTAINER_VERSION` (script constant) and a
  freshly-recomputed hash of the on-disk Dockerfile.
  - version match + hash match → proceed, no rebuild.
  - version match + hash **mismatch** → **"DEVELOPER ERROR: Dockerfile
    modified without version bump"**, forces a rebuild anyway but prints a
    scary warning (`common.bash:500-517`).
  - version **mismatch** → normal "container version update required", silent
    rebuild (`common.bash:519-531`).
  - version mismatch is the *intended*, clean path for shipping a new engine:
    bump `claude-yolo-version` (and `REQUIRED_CONTAINER_VERSION` to match) and
    users auto-rebuild on next `ccy` launch with no scary banner.
- **Consequence if you forget**: every user's next `ccy` run either silently
  rebuilds without picking up your new Dockerfile content (if you only bumped
  one side and got lucky) or hits the "DEVELOPER ERROR" banner
  (`ContainerRules.md`: "A pre-commit hook enforces this requirement" for the
  `claude-yolo` script's own `CCY_VERSION`/`CCY_HASH` pair — the analogous
  Dockerfile pair is validated at **runtime by every user**, not by a
  pre-commit hook, so it fails later and more visibly if missed).

`CLAUDE/ContainerRules.md` "CCY Version Bump Requirement" section documents
the semver convention to use (patch/minor/major) — adding a new browser
engine is unambiguously **at least a minor bump** ("New features, backward
compatible changes").

### 3b. `claude-yolo` wrapper script pair (only if the wrapper script itself changes)

| Value                                                                                                                     | File             | Current value found |
| ------------------------------------------------------------------------------------------------------------------------- | ---------------- | ------------------- |
| `CCY_VERSION`                                                                                                             | `claude-yolo:17` | `"3.29.0"`          |
| (self-validated via `CCY_HASH`, computed at runtime from the script's own content, excluding the `CCY_HASH=` line itself) | `claude-yolo:55` | n/a (derived)       |

This pair is validated by `load_launch_config()` (`claude-yolo:294-370`)
against the user's **saved per-project launch config**
(`.claude/ccy/.last-launch.conf`) — it governs SSH-key/token/network reuse
prompts, not the container image. It only needs bumping if you add a new
`ccy` CLI flag (e.g. a flag to pick which browser engine to launch, or an
`--engine-browser` selector) or otherwise edit the script's logic. A pure
Dockerfile-only addition (new engine baked into the image, driven only by a
skill/CLI invoked from inside Claude Code) does **not** require touching
`CCY_VERSION`.

**Rule from `CLAUDE/ContainerRules.md`**: *"ALWAYS bump CCY_VERSION when
modifying `files/var/local/claude-yolo/claude-yolo`"* — enforced by a
pre-commit hook (build-time in the repo, not runtime like §3a).

---

## 4. Every file that must change to add a new browser engine + its skill

Based on how `agent-browser`/Chromium is wired in today (the direct
precedent), by file:

1. **`files/var/local/claude-yolo/Dockerfile`**

   - Add the new engine's OS-level deps (apt layer, ideally its own
     `RUN apt-get install …` block mirroring `Dockerfile:132-152`, placed in
     `base` so `claude-yolo:base` consumers get it too — or in `full` if it's
     meant to be an opt-in extra, matching how LSPs are `full`-only).
   - Add the engine install itself (npm/binary-download/from-source-via-a-new-
     builder-stage, per §2).
   - Add engine config (if headed-Wayland behaviour needs a config file, the
     `agent-browser` precedent is `Dockerfile:158-164`, `/root/.agent-browser/config.json`
     baked at build time with the ozone/Wayland/no-sandbox flags).
   - **Bump `LABEL claude-yolo-version`** (§3a).

2. **`files/var/local/claude-yolo/claude-yolo`**

   - **Bump `REQUIRED_CONTAINER_VERSION`** to the identical new value (§3a).
   - Only if adding a CLI flag / engine selector / new GUI-mount requirement:
     edit the arg-parsing loop (`claude-yolo:441-555`), the `--help` text
     (`claude-yolo:135-270`), and **bump `CCY_VERSION`** (§3b).
   - Only if the new engine needs additional host resources beyond the
     existing Wayland-socket mount (e.g. a GPU device node for hardware
     acceleration, additional env vars) — extend `GUI_MOUNTS`
     (`claude-yolo:2706-2732`). The existing mechanism is engine-agnostic
     (it activates whenever `$WAYLAND_DISPLAY`/`$DISPLAY` is set, regardless
     of which browser consumes the socket), so a headed engine that just wants
     the same socket needs **no script change at all** here.

3. **`files/opt/claude-yolo/skills/<new-skill>/SKILL.md`** (+ sibling
   `COMMANDLINE-USAGE.md` / `EXAMPLES.md` if following the `browsing` skill's
   three-file pattern — see `files/opt/claude-yolo/skills/browsing/`) — a new
   skill directory teaching the agent the new engine's CLI, structured like
   the existing `browsing` skill (`SKILL.md` front-matter: `name`,
   `description`, `allowed-tools: Bash`).

   - Decide whether this **replaces** or **supplements** the `browsing`
     skill's guidance — if both engines coexist, the skill(s) need to make
     clear when to reach for the lightweight engine vs. Chromium (e.g. "use
     the lightweight engine for JS-rendered content extraction; use
     agent-browser when you need full interaction fidelity, extensions, or a
     visible window").

4. **`playbooks/imports/play-claude-yolo.yml`** — the Ansible play that
   copies all the above into the Docker build context on the host:

   - `Copy Dockerfile` task (`play-claude-yolo.yml:86-96`) already copies the
     whole Dockerfile — no new task needed unless the new engine ships extra
     files that must land in the Docker build context outside the Dockerfile
     itself (e.g. a config file the `COPY` instruction references, matching
     how `ccy-startup-info.txt` / `docs/*.txt` / `plugins/phpantom-lsp/` /
     `skills/browsing/*` are each given their own `Create Directory` +
     `Copy … into Docker Build Context` task pair, `play-claude-yolo.yml:135-201`).
   - **New skill files need their own task pair**, mirroring
     `play-claude-yolo.yml:175-201` (`Create Skills Directory` / `Copy Browsing Skills into Docker Build Context`) — a new
     `Create <Engine> Skill Directory` + `Copy <Engine> Skill into Docker Build Context` targeting `/opt/claude-yolo/skills/<new-skill>/`.
   - **Installation Summary banner** (`play-claude-yolo.yml:453-508`) should
     get a line under "✅ CCY" naming the new engine, mirroring
     `"   • Browser: agent-browser (Chromium) built in"` (`:464`).

5. **`files/opt/claude-yolo/ccy-startup-info.txt`** — the in-container banner
   printed at Claude Code startup. Currently lists `agent-browser` under "CLI
   TOOLS" (`ccy-startup-info.txt:25-26`). A new engine's CLI binary/command
   should get an equivalent one-line entry so the agent discovers it without
   having to be told.

6. **`files/opt/claude-yolo/docs/CCY-GUIDE.txt`** (referenced but not read in
   this pass — `play-claude-yolo.yml:169` copies it into the image at
   `/opt/claude-yolo/docs/CCY-GUIDE.txt`; `ccy-startup-info.txt:29` tells the
   agent to `cat` it for the "full system guide"). If it documents installed
   tooling in more depth than the startup banner, it likely needs an entry
   too — **not independently verified in this pass; flag for follow-up.**

7. **`docs/containerization.md`** — human-facing docs. The "What's already
   included in the base image" bullet list (`docs/containerization.md:438-442`)
   names `agent-browser` explicitly; a new engine should be added there. Also
   candidate for a note in "Custom Dockerfiles" best-practices if the new
   engine changes image-size guidance.

8. **`CLAUDE/ContainerRules.md`** — only needs an edit if the new engine
   introduces its own fragile/best-effort patch akin to the ctrl+z SIGSTOP
   patch (unlikely unless the engine also embeds a forked Node/Ink-style TUI).
   Not expected to need changes for a standard browser-engine addition.

---

## 5. QA gates that apply

Checked against `CLAUDE/QA.md` and the actual `scripts/qa-*.bash` inventory
(`ls /workspace/scripts/`: `qa-all.bash`, `qa-ansible-syntax.bash`,
`qa-ansible.bash`, `qa-bash.bash`, `qa-ccy`, `qa-ctrl-z-patch.bash`,
`qa-helper-tests.bash`, `qa-js.bash`, `qa-nokill-containerwatch.bash`,
`qa-patterns.bash`, `qa-python.bash`):

- **`./scripts/qa-all.bash`** — required because `playbooks/imports/play-claude-yolo.yml`
  is Ansible YAML (gates: fail-fast grep, `ansible-playbook --syntax-check`,
  playbook shebang/exec-bit hygiene) and because any new skill/wrapper glue
  written in Bash/Python is covered by its bash/python sub-stages.
- **No dedicated Dockerfile linter exists in this repo.** There is no
  `hadolint` (or equivalent) invocation anywhere under `scripts/`. A
  Dockerfile change is **not statically QA'd** beyond what a human reviews —
  the only automated guard on the Dockerfile is the runtime
  version/hash-mismatch check in `validate_container_version()` (§3a), which
  catches a **missed version bump**, not a broken or wasteful Dockerfile
  instruction. This is a gap worth naming explicitly since it means a bad
  `RUN` layer (e.g. one that fails to clean apt lists, or duplicates an
  existing dependency) will not be caught by `qa-all.bash`.
- **`./scripts/qa-ctrl-z-patch.bash`** applies only if
  `ccy-ctrl-z-patch.js` itself changes — not expected for a browser-engine
  addition unless the new engine also ships a Node-based CLI whose Ink/TUI
  layer needs the same ctrl+z workaround.
- **ESLint** (`cd extensions && node_modules/.bin/eslint <file>`) is
  irrelevant here — that gate covers GNOME Shell extension JS under
  `extensions/`, not CCY container content.
- No skill-file-specific QA gate exists (`SKILL.md` files are not linted by
  any `scripts/qa-*` stage found).

---

## 6. Current cost baseline (established from the repo where possible)

| Line item                                | Measured value                                                                                                                                                                                                                                                                                                                                                                                                                   | Source                                                                                                                                                                                                                                       | Notes                                                                                                                                                                                                                                                                                                      |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Chromium runtime apt packages            | **17** packages                                                                                                                                                                                                                                                                                                                                                                                                                  | `Dockerfile:133-151`: `libnss3, libnspr4, libatk1.0-0, libatk-bridge2.0-0, libcups2, libdrm2, libxkbcommon0, libxcomposite1, libxdamage1, libxfixes3, libxrandr2, libgbm1, libasound2, libpango-1.0-0, libcairo2, libatspi2.0-0, libgtk-3-0` | counted directly from the `apt-get install` list, one dedicated `RUN` layer with `--no-install-recommends` + `apt-get clean` + list cleanup in the same layer                                                                                                                                              |
| What `agent-browser install` pulls       | **UNKNOWN — not established from the repo.** `Dockerfile:155-156` runs `npm install -g agent-browser && agent-browser install` as a single `RUN`, but the actual download size/count of the Chromium build it fetches is not visible in any repo file — it is whatever `agent-browser install` (a Playwright-based installer, per the task brief) does at build time, over the network, and is not pinned/vendored in this repo. | `Dockerfile:154-156`                                                                                                                                                                                                                         | To get a real number, the build would need to be run and the resulting layer size measured (`podman/docker history claude-yolo:base`) — out of scope for this repo-only pass; flagged rather than guessed                                                                                                  |
| Total apt install layers before Chromium | 4 separate `apt-get install` layers (general CLI, doc/image tools, GitHub CLI's own layer, Chromium libs) each `apt-get clean && rm -rf /var/lib/apt/lists/*` in the same `RUN`                                                                                                                                                                                                                                                  | `Dockerfile:50-98, 100-107, 133-152`                                                                                                                                                                                                         | Each is its own cache layer — a new engine's apt deps should follow the same one-`RUN`-per-concern + clean-in-layer pattern                                                                                                                                                                                |
| Node.js/npm-installed CLIs in `base`     | Claude Code, agent-browser (2 npm -g installs)                                                                                                                                                                                                                                                                                                                                                                                   | `Dockerfile:130, 155`                                                                                                                                                                                                                        | `full` stage adds 4 more npm -g installs (LSPs) on top                                                                                                                                                                                                                                                     |
| Build-time compiler toolchain precedent  | `rust:slim` + `musl-tools` + `cargo build --release` in a **separate discarded stage** (`phpantom-builder`), only its static output binary is `COPY`'d into `full`                                                                                                                                                                                                                                                               | `Dockerfile:7-17, 239-240`                                                                                                                                                                                                                   | Confirms a from-source engine's *build* cost never lands in the shipped image, only its *artifact* does — relevant to any "needs a compiler toolchain at build time" disqualifier: it is not actually disqualifying by itself, only relevant if the toolchain can't cross-compile a static/portable output |

---

## 7. Summary of hard constraints (for the 15-line handback)

See final assistant message.
