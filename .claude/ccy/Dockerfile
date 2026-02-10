# Fedora Desktop Ansible Project Container
#
# Customized container for fedora-desktop Ansible project
# Extends claude-yolo with Ansible, development tools, and utilities
# used by the Fedora desktop configuration playbooks

FROM claude-yolo:latest

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
# This is a library dependency, not a CLI tool, so use pip
RUN pip3 install --no-cache-dir --break-system-packages jmespath

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
RUN wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 \
    && chmod +x /usr/local/bin/yq

# Install ripgrep (rg) - modern grep alternative
RUN wget -qO /tmp/ripgrep.deb https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep_14.1.1-1_amd64.deb \
    && dpkg -i /tmp/ripgrep.deb \
    && rm /tmp/ripgrep.deb

# Install Ansible collections required by the project
RUN ansible-galaxy collection install community.general ansible.posix

# Verify installations
RUN ansible --version && \
    ansible-lint --version && \
    gh --version && \
    yq --version && \
    rg --version
