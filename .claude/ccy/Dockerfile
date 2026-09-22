# Fedora Desktop Ansible Project Container
#
# Customized container for fedora-desktop Ansible project
# Extends claude-yolo with Ansible, development tools, and utilities
# used by the Fedora desktop configuration playbooks

ARG BASE_IMAGE=claude-yolo:latest
FROM ${BASE_IMAGE}

# Install Ansible and core development tools
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        python3-pip \
        python3-venv \
        pipx \
        git \
        jq \
        wget \
        curl \
        openssh-client \
        gnupg2 \
        python3-pytest \
        python3-numpy \
        python3-pil \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

# xz-utils is not decoration: GNU tar shells out to the `xz` binary for -J, and
# upstream shellcheck ships ONLY a .tar.xz. Without it the pinned-toolchain layer
# dies on "tar (child): xz: Cannot exec". gitleaks below survives because its
# asset is .tar.gz and gzip is built in.

# python3-pytest, python3-numpy and python3-pil exist for tests/clip_scan/,
# the one test tree in this repo that is not stdlib-only (helpers/ tests are,
# by rule). Run it with: python3 -m pytest tests/clip_scan -q

# Ensure pipx binaries are in PATH
ENV PATH="/root/.local/bin:${PATH}"

# Ansible via pipx, NOT apt: Debian 12's package is ansible-core 2.14, while the
# host this repo provisions runs ansible-core 2.19 and the QA gate's
# --syntax-check must see the 2.19 parser (CLAUDE/AgentNotes.md documents three
# 2.19-only failures). The base image used to carry this install and dropped it
# in image 2.33 because it is this project's tooling, not everyone's.
# jmespath serves json_query; passlib serves password_hash in play-ftp-camera.yml.
RUN pipx install --include-deps ansible \
    && pipx inject ansible jmespath \
    && pipx inject ansible passlib

# Install Ansible testing and linting tools using pipx
#
# ruff is PINNED (Plan 00071). Its enabled ruleset is its DEFAULT set, and that
# default grows with each release — 0.16.0 enables UP, BLE, SIM, DTZ, B and more,
# far beyond the historical E4/E7/E9/F. An unpinned ruff therefore silently
# tightens the gate on a schedule nobody in this repo controls, and main goes red
# without a commit. The versions are the single source of truth in /.qa-versions;
# scripts/qa-toolchain.bash asserts the installed tools match it, so a drift here
# fails loudly rather than changing the verdict.
#
# Every QA tool is pinned, not just ruff: the same argument applies to all of
# them. shellcheck 0.9.0 (this image's apt build) and 0.11.0 (the host's)
# reported 173 and 141 issues on the same tree, so the bash gate's verdict
# depended on where it ran. scripts/qa-toolchain.bash asserts each pinned tool
# against .qa-versions — the file is the population, so no count is repeated here.
#
# Each pinned tool is installed HERE and nowhere else in this file. pipx declines a
# package it already has rather than correcting its version, so an unpinned install
# of the same tool in an earlier layer would turn the pinned one below into a silent
# no-op and the image would carry whatever was latest at build time while claiming
# the pin. Do not add a second install site for any of these.
COPY .qa-versions /tmp/.qa-versions

# shellcheck is not a Python package so it cannot ride the pipx line, and the
# base image's apt build is years behind the host's. The upstream static binary
# goes to /usr/local/bin, which precedes /usr/bin on PATH and so shadows the apt
# one without needing it removed. It lives in the PROJECT image rather than the
# shared base because it is this repo's gate that demands the exact version
# (ContainerRules.md: project-only tooling belongs here).
#
# One RUN, so /tmp/.qa-versions is still present for both halves and is removed
# exactly once, at the end.
RUN set -eux; \
    . /tmp/.qa-versions; \
    pipx install "ruff==${RUFF}"; \
    pipx install "semgrep==${SEMGREP}"; \
    curl -fsSL -o /tmp/shellcheck.tar.xz \
        "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK}/shellcheck-v${SHELLCHECK}.linux.x86_64.tar.xz"; \
    tar -xJf /tmp/shellcheck.tar.xz -C /tmp; \
    install -m 0755 "/tmp/shellcheck-v${SHELLCHECK}/shellcheck" /usr/local/bin/shellcheck; \
    rm -rf /tmp/shellcheck.tar.xz "/tmp/shellcheck-v${SHELLCHECK}"; \
    rm -f /tmp/.qa-versions

# Install GitHub CLI (gh) - used by git configuration playbook
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Install yq (YAML processor) - used extensively in playbooks
# Pinned to a specific release; update by bumping the version and rebuilding.
RUN wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/download/v4.53.3/yq_linux_amd64 \
    && chmod +x /usr/local/bin/yq

# Install gitleaks (secret scanner) - lets us reproduce the CI "gitleaks secret
# scan" job locally with `gitleaks dir .` before pushing. Pinned to the SAME
# version the QA workflow (.github/workflows/qa.yml) installs so local and CI
# results match exactly; bump both together.
ARG GITLEAKS_VERSION=8.30.1
RUN wget -qO /tmp/gitleaks.tgz "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" \
    && tar -xzf /tmp/gitleaks.tgz -C /usr/local/bin gitleaks \
    && rm /tmp/gitleaks.tgz \
    && chmod +x /usr/local/bin/gitleaks

# Install ImageMagick 7 (Debian 12 ships only IM6).
# Production target is Fedora 43 which ships IM7 natively; this is for
# prototyping recipes against the same major version the script will call.
# AppImage is extracted (no FUSE in the container); a thin wrapper at
# /usr/local/bin/magick fronts the bundled binary with the right
# LD_LIBRARY_PATH and MAGICK_CONFIGURE_PATH.
# Pinned to a GitHub release AppImage (the old unversioned
# imagemagick.org/archive/binaries/magick URL was removed upstream and now
# 404s). Bump IMAGEMAGICK_VERSION to update; see the releases list at
# https://github.com/ImageMagick/ImageMagick/releases
ARG IMAGEMAGICK_VERSION=7.1.2-26
RUN wget -qO /tmp/magick.AppImage "https://github.com/ImageMagick/ImageMagick/releases/download/${IMAGEMAGICK_VERSION}/ImageMagick-${IMAGEMAGICK_VERSION}-gcc-x86_64.AppImage" \
    && chmod +x /tmp/magick.AppImage \
    && cd /opt \
    && /tmp/magick.AppImage --appimage-extract > /dev/null \
    && mv squashfs-root imagemagick7 \
    && rm /tmp/magick.AppImage \
    && printf '%s\n' \
        '#!/bin/bash' \
        'export LD_LIBRARY_PATH="/opt/imagemagick7/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"' \
        'export MAGICK_CONFIGURE_PATH="/opt/imagemagick7/usr/etc/ImageMagick-7:/opt/imagemagick7/usr/share/ImageMagick-7"' \
        'exec /opt/imagemagick7/usr/bin/magick "$@"' \
        > /usr/local/bin/magick \
    && chmod 755 /usr/local/bin/magick \
    && magick --version | head -n1

# Install Ansible collections required by the project.
# Versions pinned to known-good ranges (current latest: community.general 13.0.1,
# ansible.posix 2.2.0); bump upper bounds when the project has been tested against
# the next major.
RUN ansible-galaxy collection install \
    "community.general>=9.0.0,<14.0.0" \
    "ansible.posix>=1.5.0,<3.0.0"

# Create mount points for read-only host filesystem access (debugging)
# These will be bound at runtime via CCY --extra-mounts flag
RUN mkdir -p /host-debug-logs \
             /host-bin \
             /host-config

# Verify installations
#
# ansible-lint and yamllint are deliberately absent: nothing in this repo invokes
# either one. Verifying a tool no layer installs is how this block turns a tidy-up
# into a build failure, so the list tracks what is actually installed above.
RUN ansible --version && \
    ruff --version && \
    semgrep --version && \
    shellcheck --version && \
    gh --version && \
    yq --version && \
    gitleaks version && \
    rg --version && \
    magick --version
