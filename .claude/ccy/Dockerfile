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
        ansible \
        python3-pip \
        python3-venv \
        pipx \
        git \
        jq \
        wget \
        curl \
        openssh-client \
        gnupg2 \
    && rm -rf /var/lib/apt/lists/*

# Ensure pipx binaries are in PATH
ENV PATH="/root/.local/bin:${PATH}"

# Install Ansible testing and linting tools using pipx
RUN pipx install ansible-lint && \
    pipx install yamllint && \
    pipx install ruff

# Install jmespath library for Ansible json_query filter
# This is a library dependency, not a CLI tool, so use pip.
# --break-system-packages is safe here: this is an isolated container image
# layer with no system Python packages that could be corrupted.
RUN pip3 install --no-cache-dir --break-system-packages "jmespath>=1.0,<2"

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
RUN ansible --version && \
    ansible-lint --version && \
    gh --version && \
    yq --version && \
    gitleaks version && \
    rg --version && \
    magick --version
