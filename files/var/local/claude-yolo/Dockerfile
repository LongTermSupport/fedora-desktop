FROM node:20-slim

# Metadata
LABEL claude-yolo-version="1.0"
LABEL description="Claude Code YOLO Mode - Isolated container for dangerously-skip-permissions"

# Install system dependencies (excluding git and gh for safety)
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
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install yq (YAML processor)
RUN wget -qO /usr/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 \
    && chmod +x /usr/bin/yq

# Install Claude Code globally
# Note: This will be updated at runtime by the wrapper script if needed
RUN npm install -g @anthropic-ai/claude-code

# Create home directory structure - writable by any user
# The container runs with --user flag matching host UID/GID
# Don't create subdirectories - let the entrypoint do it as the correct user
RUN mkdir -p /home/claude && \
    chmod 777 /home/claude

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
