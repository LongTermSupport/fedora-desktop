FROM node:20-slim

# Metadata
LABEL claude-yolo-version="1.0"
LABEL description="Claude Code YOLO Mode - Isolated container for dangerously-skip-permissions"

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

# Install Claude Code globally
# Note: This will be updated at runtime by the wrapper script if needed
RUN npm install -g @anthropic-ai/claude-code

# Copy and set up entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Set workspace directory
WORKDIR /workspace

# Note: USER directive is NOT set here - we use --user flag at runtime
# This allows dynamic UID/GID mapping to match the host user

# Use entrypoint to handle config setup
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Default command (will be overridden by wrapper)
CMD ["claude", "--help"]
