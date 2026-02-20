FROM node:20-slim

# Accept build arg for Dockerfile hash (calculated by wrapper at build time)
ARG DOCKERFILE_HASH="unknown"

# Metadata
# IMPORTANT: When modifying this Dockerfile, ALWAYS increment claude-yolo-version!
# The hash validates Dockerfile integrity - if hash changes but version doesn't,
# users will get a warning that version wasn't properly updated.
# ⚠️  THIS VALUE MUST MATCH REQUIRED_CONTAINER_VERSION in claude-yolo script!
#     If they don't match, users get infinite rebuild loops.
#     Update BOTH files together: this label AND the script variable.
LABEL claude-yolo-version="2.2"
LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"
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
    python3 \
    python3-pip \
    python3-yaml \
    tini \
    tree \
    shellcheck \
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

# Install Claude Code via npm
# installMethod in .claude.json is set to "native" by entrypoint to suppress migration warnings
RUN npm install -g @anthropic-ai/claude-code

# Patch Ink's hardcoded ctrl+z handler in Claude Code to respect CCY_DISABLE_SUSPEND env var.
# Ink intercepts ctrl+z BEFORE the keybinding system and calls process.kill(pid, 'SIGSTOP')
# which is unblockable. The flag fG5=process.platform!=="win32" controls this path.
# We extend it to also check !process.env.CCY_DISABLE_SUSPEND so the entrypoint can disable it.
RUN node -e " \
  const fs = require('fs'); \
  const p = '/usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js'; \
  let src = fs.readFileSync(p, 'utf8'); \
  const orig = 'fG5=process.platform!==\"win32\"'; \
  if (!src.includes(orig)) { process.stderr.write('CCY PATCH ERROR: ctrl+z patch target not found - Claude Code internals may have changed\n'); process.exit(1); } \
  src = src.replace(orig, 'fG5=process.platform!==\"win32\"&&!process.env.CCY_DISABLE_SUSPEND'); \
  fs.writeFileSync(p, src); \
  process.stdout.write('CCY PATCH: ctrl+z suspend patch applied\n'); \
"

# Copy and set up entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Copy startup info and documentation
COPY ccy-startup-info.txt /opt/claude-yolo/ccy-startup-info.txt
RUN chmod 444 /opt/claude-yolo/ccy-startup-info.txt

RUN mkdir -p /opt/claude-yolo/docs
COPY docs/CUSTOM-DOCKERFILES.txt /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt
RUN chmod 444 /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt

# Set workspace directory
WORKDIR /workspace

# Help tools detect container environment
ENV DEVCONTAINER=true

# Suppress "switched from npm to native installer" warning banner
# This env var gates the jfq() warning hook in Claude Code's cli.js
ENV DISABLE_INSTALLATION_CHECKS=1

# Note: USER directive is NOT set here - we use --user flag at runtime
# This allows dynamic UID/GID mapping to match the host user

# Use tini as PID 1 to properly handle signals and reap zombie processes
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]

# Default command (will be overridden by wrapper)
CMD ["claude", "--help"]
