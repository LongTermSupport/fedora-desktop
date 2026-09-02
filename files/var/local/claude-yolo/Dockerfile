# ============================================================================
# Build stage: compile PHPantom LSP from source with musl for a static binary
# ============================================================================
# The pre-built PHPantom binary requires GLIBC 2.39 (built on Ubuntu 24.04),
# but our runtime base image is Debian 12 bookworm (GLIBC 2.36).
# Building with musl produces a fully static binary that works on any Linux.
FROM rust:slim AS phpantom-builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    musl-tools \
    git \
    && rm -rf /var/lib/apt/lists/*
RUN rustup target add x86_64-unknown-linux-musl
# Pin to a specific release tag for reproducible builds
RUN git clone --depth 1 --branch 0.7.0 https://github.com/AJenbo/phpantom_lsp.git /build
WORKDIR /build
RUN cargo build --release --target x86_64-unknown-linux-musl
# The static binary will be copied into the runtime image below

# Track the latest Node LTS rather than pinning a major. Claude Code raises its
# minimum Node engine over time (e.g. 2.1.x requires >=22); a floating LTS tag
# auto-satisfies that at build time so we don't chase it by hand. node:lts-slim
# is Debian-based, matching the apt layers below; the PHPantom LSP is built
# static with musl (above) so it stays portable even if the base Debian moves.
FROM node:lts-slim AS base

# Accept build arg for Dockerfile hash (calculated by wrapper at build time)
ARG DOCKERFILE_HASH="unknown"

# Metadata
# IMPORTANT: When modifying this Dockerfile, ALWAYS increment claude-yolo-version!
# The hash validates Dockerfile integrity - if hash changes but version doesn't,
# users will get a warning that version wasn't properly updated.
# ⚠️  THIS VALUE MUST MATCH REQUIRED_CONTAINER_VERSION in claude-yolo script!
#     If they don't match, users get infinite rebuild loops.
#     Update BOTH files together: this label AND the script variable.
LABEL claude-yolo-version="2.31"
LABEL description="Claude Code YOLO Mode - Isolated container for dangerously-skip-permissions"
# NOTE: the claude-yolo-dockerfile-hash LABEL is set at the END of the final `full` stage
# (PERF-02) so a Dockerfile edit — which changes DOCKERFILE_HASH — only invalidates that
# last label layer instead of cascading a full rebuild of every apt/npm layer below.

# Disable apt's _apt sandbox download user. In rootless podman the chown of
# cache dirs to _apt fails with EPERM, and the downloaded .deb files are then
# unreadable to _apt, breaking apt-get install. Running apt downloads as root
# inside the build container is safe and is the standard workaround for
# rootless OCI builds on Debian-based base images.
RUN echo 'APT::Sandbox::User "root";' > /etc/apt/apt.conf.d/no-sandbox

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    wget \
    jq \
    ripgrep \
    fzf \
    vim \
    nano \
    less \
    bash-completion \
    htop \
    procps \
    unzip \
    ca-certificates \
    git \
    openssh-client \
    python3 \
    python3-pip \
    python3-venv \
    python3-yaml \
    python3-numpy \
    python3-pytest \
    pipx \
    tini \
    tree \
    shellcheck \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install image/PDF processing and web development utilities
# poppler-utils: pdftotext, pdfinfo, pdftoppm, pdfimages (PDF text/image extraction)
# imagemagick:   convert, identify, mogrify (image conversion, resize, inspection)
# ghostscript:   gs (PDF/PS rendering; required by imagemagick for PDF operations)
# sqlite3:       sqlite3 CLI (inspect SQLite databases common in web apps)
# dnsutils:      dig, nslookup, host (DNS debugging)
# netcat-openbsd: nc (port checking, raw HTTP testing)
# moreutils:     sponge, ts, vidir, ifne, parallel (extra Unix utilities)
# pv:            pipe viewer (monitor progress of large data pipelines)
# libimage-exiftool-perl: exiftool (read/write metadata from images, PDFs, video)
RUN apt-get update && apt-get install -y --no-install-recommends \
    poppler-utils \
    imagemagick \
    ghostscript \
    sqlite3 \
    dnsutils \
    netcat-openbsd \
    moreutils \
    pv \
    libimage-exiftool-perl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install yq (YAML processor)
RUN wget -qO /usr/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 \
    && chmod +x /usr/bin/yq

# Install Ansible via pipx (for playbook development and compatibility testing)
# pipx is installed via apt (python3-venv also required for pipx venv creation)
RUN pipx install --include-deps ansible \
    && pipx inject ansible jmespath \
    && pipx inject ansible passlib

# Install semgrep for QA pattern checks (qa-patterns.bash uses .semgrep/bash-conventions.yml)
RUN pipx install semgrep
ENV PATH="/root/.local/bin:${PATH}"

# Install uv (fast Python package manager, required by hooks-daemon upgrade)
RUN curl -fsSL https://astral.sh/uv/install.sh -o /tmp/uv-install.sh \
    && sh /tmp/uv-install.sh \
    && rm /tmp/uv-install.sh

# Install Claude Code via npm
# installMethod in .claude.json is set to "native" by entrypoint to suppress migration warnings
RUN npm install -g @anthropic-ai/claude-code

# Install system dependencies for Chromium (required by agent-browser)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    libpango-1.0-0 \
    libcairo2 \
    libatspi2.0-0 \
    libgtk-3-0 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install agent-browser - token-efficient browser automation CLI for AI agents
RUN npm install -g agent-browser && \
    agent-browser install

# Configure agent-browser with Wayland/Docker display args
# Without this, every invocation requires --headed --args "..." flags
RUN mkdir -p /root/.agent-browser && \
    echo '{\n\
  "headed": true,\n\
  "args": "--enable-features=UseOzonePlatform,--ozone-platform=wayland,--no-sandbox,--disable-gpu,--disable-software-rasterizer,--disable-dev-shm-usage"\n\
}' > /root/.agent-browser/config.json

# Reap abandoned browsers. agent-browser's daemon shuts itself down after this much
# inactivity, and upstream's default is ONE HOUR (3600000) — so a window an agent opened
# and forgot sat on the user's desktop for an hour. Five minutes is long enough for an
# agent to think between commands and short enough that a forgotten window is gone before
# anyone has to notice it. Measured in-container: with a 4s value the daemon was gone
# within 7s of its last command.
#
# This is the safety net, not the cleanup: the browsing skill requires the agent to close
# what it opened in the same command chain. A project that genuinely needs a longer window
# — say the user is watching a headed session — overrides this in its ccy.env, which is
# sourced after image ENV and wins. Applies to both engines; each daemon reads it.
ENV AGENT_BROWSER_IDLE_TIMEOUT_MS=300000

# Install Lightpanda — a SECOND browser engine, driven by the agent-browser we already
# have. agent-browser ships a native `--engine lightpanda` flag; all that is missing from
# the image is the binary, so this is a download rather than a parallel browser stack.
#
# Why it earns its place (measured in a CCY container, not vendor claims — see
# CLAUDE/Plan/00097-lightweight-agent-browser-engine/logs/browser-engine-triage.log):
# on the same page via the same CLI, Lightpanda 0.3.6 returned identical text in
# 379 ms / 1 process / ~25 MB peak RSS where Chromium 150 took 1177 ms / 15 processes /
# ~1345 MB, and it matched Chromium on all 8 JS-capability fixtures — fetch(), ES
# modules, custom elements + shadow DOM, and a React 18 client render included.
#
# What it CANNOT do: there is no CSS layout or paint pipeline. `screenshot` exits 0 and
# writes a placeholder PNG; `get box` exits 0 and returns fabricated geometry. Both fail
# SILENTLY, which is why Chromium stays the default and this engine is opt-in via
# agent-browser-lite. See the browsing skill for the rule the agent follows.
#
# The checksum is the sha256 GitHub publishes for this exact release asset, so a
# re-tagged or substituted artifact fails the build rather than shipping quietly.
ARG LIGHTPANDA_VERSION="0.3.6"
ARG LIGHTPANDA_SHA256="e438c0ad44e0f6916c14cf13beb003512c60438d8fd200738d2e596e73f652d6"
RUN curl -fsSL -o /tmp/lightpanda \
    "https://github.com/lightpanda-io/browser/releases/download/${LIGHTPANDA_VERSION}/lightpanda-x86_64-linux" \
    && echo "${LIGHTPANDA_SHA256}  /tmp/lightpanda" | sha256sum -c - \
    && install -m 0755 /tmp/lightpanda /usr/local/bin/lightpanda \
    && rm -f /tmp/lightpanda \
    && lightpanda version

# Lightpanda needs its OWN agent-browser config. The Chromium config written above sets
# "headed": true and Chromium-specific "args", and the Lightpanda code path rejects both
# outright ("Headed mode is not supported with Lightpanda", "Custom Chrome arguments are
# not supported with Lightpanda") — so without this file the engine is unreachable even
# once the binary exists. Note that env vars do NOT rescue it: AGENT_BROWSER_HEADED=false
# was measured failing to override "headed": true from the config file, despite the
# documented precedence, so a separate --config is the only mechanism that works.
RUN printf '%s\n' \
    '{' \
    '  "engine": "lightpanda",' \
    '  "executablePath": "/usr/local/bin/lightpanda",' \
    '  "headed": false' \
    '}' > /root/.agent-browser/lightpanda.json

# agent-browser-lite — the Lightpanda engine as one word instead of a long --config flag.
# A passthrough: every agent-browser subcommand, flag and exit code behaves identically,
# and stdout stays the wrapped command's alone.
#
# --namespace is NOT cosmetic, it is what makes the two engines coexist. agent-browser
# keeps a per-namespace daemon, and a daemon is bound to the engine it was started with.
# Sharing the default namespace means the FIRST engine used wins until its daemon is
# fully down, so an agent that reads a page with the light engine and then reaches for
# Chromium gets "Custom Chrome arguments (--args) are not supported with Lightpanda"
# (reproduced 3/3 in acceptance) — and `close --all` does not fix it, because the close
# returns before the daemon has gone. That directly contradicts the browsing skill's
# "fall back freely, the syntax is identical". A dedicated namespace gives each engine
# its own daemon socket, so the two interleave in any order with no closes at all.
RUN printf '%s\n' \
    '#!/bin/sh' \
    '# agent-browser, driven by the Lightpanda engine instead of Chromium.' \
    '#' \
    '# Use for: reading pages, extracting text/markdown, scraping, JS-rendered SPAs.' \
    '# Do NOT use for: screenshots, PDFs, element geometry, or anything visual —' \
    '# Lightpanda has no layout/paint pipeline and fails SILENTLY (exit 0 plus a' \
    '# placeholder image or fabricated coordinates). Use plain agent-browser there.' \
    '#' \
    '# --namespace keeps this engine on its own daemon socket; without it the two' \
    '# engines fight over one daemon and whichever ran first wins. Do not remove it.' \
    'exec agent-browser --namespace lightpanda \' \
    '    --config /root/.agent-browser/lightpanda.json "$@"' \
    > /usr/local/bin/agent-browser-lite \
    && chmod 755 /usr/local/bin/agent-browser-lite \
    && sh -n /usr/local/bin/agent-browser-lite

# Stub for common typo: agentbrowser → agent-browser
RUN printf '#!/bin/sh\necho "Command not found: agentbrowser" >&2\necho "Did you mean: agent-browser" >&2\nexit 1\n' \
    > /usr/local/bin/agentbrowser && \
    chmod +x /usr/local/bin/agentbrowser

# ctrl+z / SIGSTOP suppression is NOT done here any more. The image used to
# byte-patch Ink's hardcoded suspend handler out of Claude Code; that job now
# belongs to the hooks-daemon PTY supervisor's Ctrl+Z guard (hooks-daemon Plan
# 00173, .claude/ccy/claude-supervise.py: strip_suspend() + the SIGTSTP/SIGQUIT
# input-signal guards). See CLAUDE/ContainerRules.md.

# Copy and set up entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Copy startup info and documentation
COPY ccy-startup-info.txt /opt/claude-yolo/ccy-startup-info.txt
RUN chmod 444 /opt/claude-yolo/ccy-startup-info.txt

RUN mkdir -p /opt/claude-yolo/docs
COPY docs/CUSTOM-DOCKERFILES.txt /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt
COPY docs/CCY-GUIDE.txt /opt/claude-yolo/docs/CCY-GUIDE.txt
RUN chmod 444 /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt \
              /opt/claude-yolo/docs/CCY-GUIDE.txt

# Claude Code skills for browser automation.
#
# STAGED under /opt/claude-yolo/, NOT written straight to /root/.claude/skills/.
# entrypoint.sh replaces /root/.claude with a symlink to /workspace/.claude/ccy so
# sessions persist in the project — and it `rm -rf`s the real directory first. Anything
# baked into /root/.claude at build time is therefore DELETED on every container start,
# which is exactly what happened to this skill: it shipped in the image and no agent
# could ever load it. The entrypoint installs it after the symlink exists, the same way
# the PHPantom plugin is handled.
COPY skills/browsing/ /opt/claude-yolo/skills/browsing/

# OPT-IN tooling, staged outside skills/ on purpose (Plan 00092).
#
# entrypoint.sh copies everything under /opt/claude-yolo/skills/ into every session
# unconditionally, so a skill that must ship only when a project asks for it cannot
# live there. This tree is installed by the entrypoint only when the project sets
# CCY_CHILD_CLAUDE=1 in its ccy.env, and actively REMOVED when it does not — the
# session skills directory is host-persisted, so without the removal the mode could
# be switched on but never off.
COPY optional/ /opt/claude-yolo/optional/
RUN chmod 755 /opt/claude-yolo/optional/child-claude/bin/ccy-claude

# Set workspace directory
WORKDIR /workspace

# Help tools detect container environment
ENV DEVCONTAINER=true

# Suppress "switched from npm to native installer" warning banner
# This env var gates the jfq() warning hook in Claude Code's cli.js
ENV DISABLE_INSTALLATION_CHECKS=1

# Note: no USER directive, so the container runs as uid 0 INSIDE the container.
#
# That is safe only because ccy requires a ROOTLESS engine, where the user
# namespace maps container uid 0 to the unprivileged host user who launched it.
# The launcher asserts this against the engine's own report before starting
# anything (engine_assert_rootless, lib/common.bash) and refuses otherwise.
#
# This comment previously read "we use --user flag at runtime". No --user flag is
# passed: DOCKER_FLAGS is only ever -i/-it (claude-yolo:2696/:2698) and the run
# invocation has none. The userns mapping — not a --user flag — is what provides
# the host-side identity.

# Use tini as PID 1 to properly handle signals and reap zombie processes
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]

# Default command (will be overridden by wrapper)
CMD ["claude", "--help"]

# ============================================================================
# Full image: base + pre-bundled LSP language servers
# ============================================================================
# Projects that need a leaner image with specific LSPs can use:
#   FROM claude-yolo:base
# and install only the servers they need (see CUSTOM-DOCKERFILES.txt).
#
# Pre-bundled servers: TypeScript/JS, Python (pyright), Bash,
#   PHP: PHPantom (default, Rust-based) + Intelephense (fallback, Node-based)
#   Switch PHP LSP via enabledPlugins in .claude/settings.json
# The entrypoint auto-enables ENABLE_LSP_TOOL in Claude Code settings.json.
FROM base AS full
RUN npm install -g \
    typescript-language-server \
    typescript \
    pyright \
    bash-language-server \
    intelephense
# PHPantom LSP: static musl binary from build stage (no GLIBC dependency)
COPY --from=phpantom-builder /build/target/x86_64-unknown-linux-musl/release/phpantom_lsp /usr/local/bin/phpantom_lsp
RUN chmod 755 /usr/local/bin/phpantom_lsp
# PHPantom Claude Code plugin (registers phpantom_lsp as an LSP server for .php files)
COPY plugins/phpantom-lsp/ /opt/claude-yolo/plugins/phpantom-lsp/

# PERF-02: image-hash metadata as the very last layer. DOCKERFILE_HASH changes on every
# Dockerfile edit; keeping its only consumer here means a one-line edit no longer busts the
# apt/npm/Chromium layers above it. Re-declared because ARGs do not cross stage boundaries.
ARG DOCKERFILE_HASH="unknown"
LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"
