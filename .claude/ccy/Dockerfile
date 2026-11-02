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
        gawk \
        tmux \
        gcc \
        libc6-dev \
        fontconfig \
    && rm -rf /var/lib/apt/lists/*

# fontconfig serves scripts/test-colour-emoji-fallback.bash, which runs fc-cache, fc-match
# and fc-list against fixture fonts; play-fonts.yml installs it on the host.

# tmux serves scripts/test-ccy-tmux-targets.bash, which checks the session targets ccy and
# ccy-sessions name against a real tmux server; the other ccy-sessions suites use a fake
# one. play-claude-yolo.yml installs it on the host. Nothing in the image runs a session
# under it: ccy starts its tmux sessions on the host.

# gcc and libc6-dev serve `go test -race` on src-go/: the race detector needs cgo.

# gawk serves scripts/test-bash-history-search.bash: the bash-history ranker it tests
# is gawk, and the base image carries only mawk. play-basic-configs.yml installs it on
# the host.

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
#
# Every download in this file is chosen by the image's architecture (dpkg --print-architecture),
# mapped to each upstream's own spelling, and checked against one pinned sha256 per
# architecture (the release API's digest for that asset); any other architecture fails the
# build naming the tool. The shellcheck checksums belong to the SHELLCHECK pin in
# /.qa-versions: bump them with it.
ARG SHELLCHECK_SHA256_AMD64=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
ARG SHELLCHECK_SHA256_ARM64=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588
RUN set -eux; \
    . /tmp/.qa-versions; \
    pipx install "ruff==${RUFF}"; \
    pipx install "semgrep==${SEMGREP}"; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) sc_arch=x86_64; sc_sha="${SHELLCHECK_SHA256_AMD64}" ;; \
        arm64) sc_arch=aarch64; sc_sha="${SHELLCHECK_SHA256_ARM64}" ;; \
        *) echo "shellcheck: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/shellcheck.tar.xz \
        "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK}/shellcheck-v${SHELLCHECK}.linux.${sc_arch}.tar.xz"; \
    echo "${sc_sha}  /tmp/shellcheck.tar.xz" | sha256sum -c -; \
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
# Pinned to a specific release; bump the version and both checksums together. yq names its
# builds as dpkg does (amd64, arm64).
ARG YQ_VERSION=4.54.1
ARG YQ_SHA256_AMD64=8e34fc298390875de416e6a4afcb8cabeceb25d9aa8506c1a2f9353cf702ea5f
ARG YQ_SHA256_ARM64=189088da0c6429ec5178dfaab1a114805f6cab0b61b165ab236efedf1d57a71b
RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) yq_sha="${YQ_SHA256_AMD64}" ;; \
        arm64) yq_sha="${YQ_SHA256_ARM64}" ;; \
        *) echo "yq: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac \
    && wget -qO /tmp/yq "https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${arch}" \
    && echo "${yq_sha}  /tmp/yq" | sha256sum -c - \
    && install -m 0755 /tmp/yq /usr/local/bin/yq \
    && rm -f /tmp/yq

# Install gitleaks (secret scanner) - lets us reproduce the CI "gitleaks secret
# scan" job locally with `gitleaks dir .` before pushing. Pinned to the SAME
# version the QA workflow (.github/workflows/qa.yml) installs so local and CI
# results match exactly; bump both together.
ARG GITLEAKS_VERSION=8.30.1
ARG GITLEAKS_SHA256_AMD64=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
ARG GITLEAKS_SHA256_ARM64=e4a487ee7ccd7d3a7f7ec08657610aa3606637dab924210b3aee62570fb4b080
RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) gl_arch=x64; gl_sha="${GITLEAKS_SHA256_AMD64}" ;; \
        arm64) gl_arch=arm64; gl_sha="${GITLEAKS_SHA256_ARM64}" ;; \
        *) echo "gitleaks: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac \
    && wget -qO /tmp/gitleaks.tgz "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_${gl_arch}.tar.gz" \
    && echo "${gl_sha}  /tmp/gitleaks.tgz" | sha256sum -c - \
    && tar -xzf /tmp/gitleaks.tgz -C /usr/local/bin gitleaks \
    && rm /tmp/gitleaks.tgz \
    && chmod +x /usr/local/bin/gitleaks

# gum serves ccy-teams (src-go/cmd/ccy-teams), whose create flow asks with
# it, and lets its prompts be tried here against the real tool. Debian 12 has no package,
# so the upstream binary is pinned to the version Fedora 44 ships, which is what
# play-claude-yolo.yml installs on the host: the flags tested here are the flags run there.
# Bump it with the host's version.
ARG GUM_VERSION=0.17.0
ARG GUM_SHA256_AMD64=69ee169bd6387331928864e94d47ed01ef649fbfe875baed1bbf27b5377a6fdb
ARG GUM_SHA256_ARM64=b0b9ed95cbf7c8b7073f17b9591811f5c001e33c7cfd066ca83ce8a07c576f9c
RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) gum_arch=x86_64; gum_sha="${GUM_SHA256_AMD64}" ;; \
        arm64) gum_arch=arm64; gum_sha="${GUM_SHA256_ARM64}" ;; \
        *) echo "gum: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac \
    && wget -qO /tmp/gum.tgz "https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/gum_${GUM_VERSION}_Linux_${gum_arch}.tar.gz" \
    && echo "${gum_sha}  /tmp/gum.tgz" | sha256sum -c - \
    && tar -xzf /tmp/gum.tgz -C /usr/local/bin --strip-components=1 "gum_${GUM_VERSION}_Linux_${gum_arch}/gum" \
    && rm /tmp/gum.tgz \
    && chmod 755 /usr/local/bin/gum

# Go builds this repository's CLI tools (src-go/: ccy-teams first). Debian 12's golang is
# 1.19, so the upstream toolchain is pinned to the version Fedora 44 ships, which is what
# play-claude-yolo.yml builds the tools with on the host. Bump it with the host's version.
# golangci-lint is a QA tool, so its version lives in /.qa-versions like the others.
ARG GO_VERSION=1.26.10
ARG GO_SHA256_AMD64=ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5
ARG GO_SHA256_ARM64=3f4227ce7b206a513e794f1393adb473a07b46e94d18bd15d1c684bbb7319797
RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) go_sha="${GO_SHA256_AMD64}" ;; \
        arm64) go_sha="${GO_SHA256_ARM64}" ;; \
        *) echo "go: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac \
    && wget -qO /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" \
    && echo "${go_sha}  /tmp/go.tgz" | sha256sum -c - \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt
# GOTOOLCHAIN=local: go.mod's toolchain line must never make go download another one.
ENV GOTOOLCHAIN=local
# The golangci-lint checksums belong to the GOLANGCI_LINT pin in /.qa-versions: bump them with it.
ARG GOLANGCI_LINT_SHA256_AMD64=ab90aeb7b066f92a33415b638a50fe5344bbb75a0d32ad30cc248d88f81032ab
ARG GOLANGCI_LINT_SHA256_ARM64=ee7ec5f3453d15ddf106fae5a4d6c71737712348a979d1fe9cd52ec7ea299bae
COPY .qa-versions /tmp/.qa-versions
RUN set -eux; \
    . /tmp/.qa-versions; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) gl_sha="${GOLANGCI_LINT_SHA256_AMD64}" ;; \
        arm64) gl_sha="${GOLANGCI_LINT_SHA256_ARM64}" ;; \
        *) echo "golangci-lint: no build pinned for ${arch}" >&2; exit 1 ;; \
    esac; \
    gl_name="golangci-lint-${GOLANGCI_LINT}-linux-${arch}"; \
    wget -qO /tmp/gl.tgz "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT}/${gl_name}.tar.gz"; \
    echo "${gl_sha}  /tmp/gl.tgz" | sha256sum -c -; \
    tar -xzf /tmp/gl.tgz -C /usr/local/bin --strip-components=1 "${gl_name}/golangci-lint"; \
    rm -f /tmp/gl.tgz /tmp/.qa-versions; \
    golangci-lint version

# Install ImageMagick 7 at the upstream release (Debian's is an older 7.1.1).
# Production target is Fedora 43 which ships IM7 natively; this is for
# prototyping recipes against the same major version the script will call.
# AppImage is extracted (no FUSE in the container); a thin wrapper at
# /usr/local/bin/magick fronts the bundled binary with the right
# LD_LIBRARY_PATH and MAGICK_CONFIGURE_PATH.
# Pinned to a GitHub release AppImage (the old unversioned
# imagemagick.org/archive/binaries/magick URL was removed upstream and now
# 404s). Bump IMAGEMAGICK_VERSION and its checksum together; see the releases list at
# https://github.com/ImageMagick/ImageMagick/releases
#
# ImageMagick publishes no Linux arm64 build (its arm64 assets are Windows only). On arm64 the
# base image's Debian package is used instead: node:lts-slim is Debian 13, whose imagemagick is
# IM7 (7.1.1) with `magick`, and this layer fails the build unless that magick reports 7.
ARG IMAGEMAGICK_VERSION=7.1.2-26
ARG IMAGEMAGICK_SHA256_AMD64=6366d353b81871929a653ad81158e805f3611d1be7836f2490b6afc38b64c92f
RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) im_asset="ImageMagick-${IMAGEMAGICK_VERSION}-gcc-x86_64.AppImage" ;; \
        arm64) im_asset="" ;; \
        *) echo "ImageMagick 7: no build for ${arch}" >&2; exit 1 ;; \
    esac \
    && if [ -z "${im_asset}" ]; then \
        magick -version | grep -q '^Version: ImageMagick 7\.' \
        || { echo "ImageMagick 7: no AppImage for ${arch}, and the distribution's magick is not IM7" >&2; exit 1; }; \
    else \
    wget -qO /tmp/magick.AppImage "https://github.com/ImageMagick/ImageMagick/releases/download/${IMAGEMAGICK_VERSION}/${im_asset}" \
    && echo "${IMAGEMAGICK_SHA256_AMD64}  /tmp/magick.AppImage" | sha256sum -c - \
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
    && chmod 755 /usr/local/bin/magick; \
    fi \
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
    gum --version && \
    go version && \
    golangci-lint version && \
    rg --version && \
    gawk --version && \
    fc-match --version && \
    magick --version
