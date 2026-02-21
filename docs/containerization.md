# Containerization Technologies

This project supports three distinct containerization technologies, each serving different use cases in the development workflow.

## Quick Decision Guide

**Choose the right tool for your needs:**

**Use LXC if you need:**
- A complete Linux system with systemd
- Long-running test environments
- System service testing (networking, init, etc.)
- To test installation scripts on different distros
- Full isolation from host system

**Use Docker if you need:**
- To run web services, databases, or APIs
- CI/CD pipelines and build environments
- Production-ready container deployments
- Microservices architecture
- Kubernetes or container orchestration

**Use Distrobox if you need:**
- Interactive development shells
- GUI applications from other distros
- Ubuntu/Debian packages on Fedora
- To keep your Fedora desktop clean
- Seamless access to your home directory

**Still confused?** Jump to [Choosing the Right Technology](#choosing-the-right-technology) for detailed comparisons.

## Overview Comparison

| Technology | Type | Primary Use Case | GUI Support | Home Access | Management Complexity |
|------------|------|-----------------|-------------|-------------|---------------------|
| **LXC** | System Containers | Isolated environments, testing | Manual setup | Isolated | High |
| **Docker** | Application Containers | Services, deployments | Manual setup | Isolated | Medium |
| **Distrobox** | Development Shells | Interactive development | Automatic | Shared by default | Low |

## LXC (Linux Containers)

### What is LXC?
LXC provides system-level containerization, creating lightweight virtual machines with their own complete Linux userspace, init system, and network stack.

### Key Characteristics
- **Full system containers** - Complete Linux environment with systemd
- **Near-VM experience** - Feels like a separate operating system
- **Privileged by default** - Can run multiple services and processes
- **Network isolation** - Own IP address and network configuration
- **Long-running** - Containers stay up like traditional VMs
- **Resource overhead** - More than app containers, less than VMs

### Use Cases
- **Multi-environment testing** - Test applications in different distros
- **Isolated development** - Complete separation from host system
- **System service testing** - Test systemd services, networking configs
- **Multi-tenant hosting** - Provide isolated environments for different users/projects
- **Legacy application support** - Run older distro versions for compatibility

### Installation
Installed automatically by the main playbook:
```bash
# Already configured in playbook-main.yml
```

Key features configured:
- SELinux in permissive mode for container compatibility
- lxcbr0 network bridge in trusted firewall zone
- Insecure SSH key (`~/.ssh/id_lxc`) for container access
- DHCP configuration for automatic IP assignment
- Increased inotify limits for container operations
- Kernel modules for Docker/OpenVPN in containers

### Usage Examples
```bash
# Create a container
sudo lxc-create -n mycontainer -t download -- -d ubuntu -r jammy -a amd64

# Start container
sudo lxc-start -n mycontainer

# Get container IP
sudo lxc-info -n mycontainer

# SSH into container
ssh ubuntu@<container-ip>  # Uses ~/.ssh/id_lxc automatically

# Stop container
sudo lxc-stop -n mycontainer

# Destroy container
sudo lxc-destroy -n mycontainer
```

### Advanced: Docker-in-LXC
For isolated Docker development environments, see `play-docker-in-lxc-support.yml` in the experimental playbooks section.

This configuration:
- Loads required kernel modules (overlay, br_netfilter)
- Configures sysctl for IP forwarding and bridge netfilter
- Enables user namespaces for rootless Docker
- Installs `docker-in-lxc` command for project-based LXC containers

```bash
# Navigate to Docker project
cd ~/Projects/my-docker-app

# Create dedicated LXC container for this project
docker-in-lxc --create

# Enter the container
docker-in-lxc --enter

# Inside container: use Docker normally
docker-compose up
```

## Docker

### What is Docker?
Docker is an application containerization platform designed for packaging, distributing, and running isolated services and applications.

### Key Characteristics
- **Application-focused** - Run single services/applications
- **Immutable by design** - Build images, deploy containers from them
- **Service-oriented** - Designed for daemons, microservices, APIs
- **Root privilege separation** - User namespaces for security
- **Limited host integration** - Deliberately isolated from host
- **No GUI by default** - Requires manual X11 forwarding setup
- **Stateless preference** - Data persistence via volumes

### Use Cases
- **Web services** - Run web servers, APIs, microservices
- **Databases** - Isolated database instances
- **CI/CD pipelines** - Reproducible build and test environments
- **Production deployments** - Container orchestration with Kubernetes/Swarm
- **Application isolation** - Run conflicting versions of software
- **Distribution** - Package applications with all dependencies

### Installation
Optional playbook (run manually):
```bash
ansible-playbook playbooks/imports/optional/common/play-docker.yml
```

Configuration details:
- Adds Docker CE repository
- Installs Docker CE, CLI, containerd, buildx, compose
- Configures **rootless Docker** for security
- Sets up user namespace ID mapping
- Enables user systemd service (no sudo required)
- Configures subuid/subgid ranges for user

### Usage Examples
```bash
# Run a service
docker run -d -p 8080:80 nginx

# Build an image
docker build -t myapp:latest .

# Use compose for multi-container apps
docker-compose up -d

# View running containers
docker ps

# Execute command in container
docker exec -it container_name bash

# Clean up
docker-compose down
docker system prune -a
```

### Rootless Docker
This project configures rootless Docker, which:
- Runs Docker daemon as regular user (no root)
- Uses user namespaces for isolation
- Service managed via `systemctl --user`
- Enhanced security posture

```bash
# Check service status
systemctl --user status docker

# Restart service
systemctl --user restart docker

# View logs
journalctl --user -u docker
```

## Distrobox

### What is Distrobox?
Distrobox is a wrapper around Podman/Docker that creates seamlessly integrated development environments, making containers feel like native shell sessions.

### Key Characteristics
- **Desktop integration** - Containers blend with host system
- **Home directory shared** - Auto-mounts `~/` into container
- **GUI automatic** - X11/Wayland forwarded without configuration
- **Host filesystem access** - Can read/write anywhere on host
- **User namespace magic** - Your UID matches inside and outside
- **Development-focused** - Interactive shells, not services
- **Disposable** - Easy to create, destroy, recreate
- **No root needed** - Runs as regular user (rootless)

### The Distrobox Philosophy
Distrobox solves: **"I use Fedora, but this tool only works on Ubuntu"**

You get Ubuntu's package ecosystem and compatibility while:
- Working on your Fedora home directory
- Using your Fedora desktop
- Staying in your Fedora session
- Accessing all your host files

### Use Cases
- **Development environments** - Different distros for different projects
- **Tool compatibility** - Use Ubuntu/Debian-only tools on Fedora
- **Package availability** - Access packages not available in Fedora
- **Testing** - Test software on multiple distros without VMs
- **Clean development** - Isolate development tools from host system
- **GUI applications** - Run graphical apps from other distros

### Installation
Optional playbook (run manually):
```bash
ansible-playbook playbooks/imports/optional/common/play-install-distrobox.yml
```

### Basic Usage
```bash
# Create a container
distrobox create --name mydev --image ubuntu:22.04

# Enter container (get interactive shell)
distrobox enter mydev

# Inside container - feels like native system
pwd  # Shows your actual host path
ls ~/Documents  # Access your real home directory
gedit file.txt  # Opens GUI app on your desktop!

# Exit container
exit

# List containers
distrobox list

# Remove container
distrobox rm mydev
```

### The Magic of Distrobox

When you enter a distrobox:

```bash
# On Fedora host
whoami  # user
pwd     # ~/Projects/my-app

distrobox enter ubuntu-dev

# Now in Ubuntu container, but...
whoami  # user (same user!)
pwd     # ~/Projects/my-app (same path!)
ls ~/   # Your actual home directory
```

**What's shared:**
- Home directory (`~/`)
- Current directory (`$PWD`)
- User ID and group ID
- X11/Wayland display
- Audio system
- USB devices
- Network (same as host)

**What's different:**
- Package manager (apt vs dnf)
- System libraries
- Available packages
- Init system (limited)

### Custom Dockerfiles for CCY

`ccy` supports project-specific container customization through custom Dockerfiles. This allows you to extend the base container with additional tools, languages, and dependencies needed for your specific project.

#### When to Use Custom Dockerfiles

Create a custom Dockerfile when you need:
- **Additional languages**: Python 3.12, Go, Rust, Ruby, Java
- **Build tools**: make, cmake, gradle, maven, specific compiler versions
- **Database clients**: postgresql-client, mysql-client, mongodb tools
- **Cloud CLIs**: aws-cli, gcloud, azure-cli, terraform, pulumi
- **Development tools**: Language-specific linters, formatters, test frameworks
- **System libraries**: Dependencies for native extensions

#### Two Approaches Available

**Quick Template-Based (`--custom`)**:
```bash
cd ~/Projects/my-project
ccy --custom

# Interactive menu:
# 1. Select template (ansible/golang/project-template)
# 2. Choose: Claude customization, manual edit, or use as-is
# 3. Quick setup for known tech stacks
```

**AI-Guided Planning (`--custom-docker`)**:
```bash
cd ~/Projects/my-project
ccy --custom-docker

# Comprehensive workflow:
# 1. Claude enters planning mode
# 2. Investigates project files (package.json, go.mod, pyproject.toml, etc.)
# 3. Asks targeted questions about your needs
# 4. Proposes features for approval
# 5. Creates optimized Dockerfile with validation
# 6. Provides clear next steps
```

#### How It Works

**Dockerfile Fallback Priority**

This is the key thing to understand. When `ccy` starts, it looks for a Dockerfile in this order:

| Priority | `ccy` looks for | Result |
|----------|-----------------|--------|
| 1 (highest) | `.claude/ccy/Dockerfile` | Custom project image |
| 2 (default) | *(none found)* | Base image only |

**Built image names:**
- `ccy` builds: `claude-yolo:<project-name>`
- Automatic rebuilds when Dockerfile changes detected
- Fast rebuilds with cache mounts

**What's already included in the base image:**
- Node.js 20, npm, Python 3, git, gh CLI, Claude Code (latest)
- Development tools: ripgrep, jq, yq, vim
- agent-browser CLI for token-efficient browser automation via Chromium

#### Example Workflow

**Creating a Custom Dockerfile for a Python Project:**

```bash
cd ~/Projects/my-python-app
ccy --custom-docker

# Claude investigates:
# - Reads pyproject.toml (finds Python 3.12, poetry, pytest, black, mypy)
# - Checks CI configs (finds AWS deployments)
# - Scans for database usage (finds PostgreSQL)

# Claude asks:
# - "I see Python 3.12 in pyproject.toml. Confirm this version?"
# - "Do you need PostgreSQL client for local development?"
# - "Should I pre-install black, mypy, pytest?"
# - "I see AWS in your CI. Need aws-cli in container?"

# Claude proposes:
# ✓ Python 3.12 + pip
# ✓ poetry, pytest, black, mypy
# ✓ PostgreSQL client 15
# ✓ AWS CLI v2
# ✓ Cache mounts for pip and apt
# ✓ Environment variables (PYTHONUNBUFFERED=1)

# You approve, Claude creates .claude/ccy/Dockerfile with:
# - Comprehensive comments explaining each section
# - Optimized cache mounts for fast rebuilds
# - Version verification commands
# - Proper documentation

# Next steps printed:
# 1. Exit: /exit or Ctrl+D
# 2. Rebuild: ccy --rebuild
# 3. Launch: ccy
```

**Result:**
```dockerfile
# .claude/ccy/Dockerfile
FROM claude-yolo:latest

# ============================================================================
# Python 3.12 + Development Tools
# ============================================================================
# Project uses Python 3.12 (specified in pyproject.toml)
# Installing: python3.12, poetry, pytest, black, mypy

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        python3.12 \
        python3.12-venv \
        python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Install Python tools with cache mount for faster rebuilds
RUN --mount=type=cache,target=/root/.cache/pip \
    pip3 install --upgrade pip && \
    pip3 install poetry pytest black mypy

# ============================================================================
# PostgreSQL Client
# ============================================================================
# Needed for connecting to development database

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        postgresql-client-15 \
    && rm -rf /var/lib/apt/lists/*

# ============================================================================
# AWS CLI
# ============================================================================
# Used in CI/CD deployments

RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf aws awscliv2.zip

# Environment variables
ENV PYTHONUNBUFFERED=1
ENV PIP_NO_CACHE_DIR=1

# Verify installations
RUN python3.12 --version && \
    poetry --version && \
    psql --version && \
    aws --version
```

#### Updating Existing Dockerfiles

If a Dockerfile already exists, `--custom-docker` offers two options:

**Analyze and Improve:**
```bash
ccy --custom-docker
# Option 1: Analyze and propose improvements

# Claude will:
# - Read current Dockerfile
# - Check project for new dependencies
# - Propose additions, updates, optimizations
# - Update Dockerfile after approval
```

**Replace with New:**
```bash
ccy --custom-docker
# Option 2: Replace with new

# Starts fresh creation workflow
# Old Dockerfile backed up automatically
```

#### Best Practices

**Cache Mounts for Speed:**
```dockerfile
# Good - Uses cache mounts (30 second rebuilds)
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y python3

# Bad - No cache (5 minute rebuilds)
RUN apt-get update && apt-get install -y python3
```

**Keep It Minimal:**
```dockerfile
# Good - Only install tools
RUN apt-get install -y golang-1.21

# Bad - Don't install project dependencies (they're in mounted /workspace)
RUN npm install  # ❌ Wrong - npm install happens at runtime, not build time
RUN go mod download  # ❌ Wrong - happens at runtime
```

**Document Everything:**
```dockerfile
# Good - Explains why
# PostgreSQL Client for development database
# Version 15 matches production (see docker-compose.yml)
RUN apt-get install -y postgresql-client-15

# Bad - No context
RUN apt-get install -y postgresql-client-15
```

**Verify Installations:**
```dockerfile
# Good - Catches build failures early
RUN go version && golangci-lint --version

# Less ideal - No verification
RUN apt-get install -y golang-1.21 golangci-lint
```

#### Saving Dockerfiles as Templates

If you create a Dockerfile you want to reuse:

```bash
# Save to template library
sudo cp .claude/ccy/Dockerfile \
  /opt/claude-yolo/custom-dockerfiles/Dockerfile.example-mystack

# Templates in /opt/claude-yolo/custom-dockerfiles/:
# - Never overwritten by Ansible
# - Appear in --custom template selection menu
# - Can be shared across projects
```

#### Troubleshooting Custom Builds

**Build fails:**
```bash
# Check Dockerfile syntax
docker build --check .claude/ccy/

# Manual build to see errors
cd .claude/ccy && docker build -t test .

# Check logs
ccy --rebuild
```

**Image too large:**
```bash
# Check image size
docker images | grep claude-yolo

# Reduce size:
# - Use --no-install-recommends with apt
# - Clean up in same RUN layer
# - Combine RUN commands when possible
# - Don't install project dependencies (npm/pip/go packages)
```

**Slow rebuilds:**
```bash
# Add cache mounts (see Best Practices above)
# - apt cache: /var/cache/apt and /var/lib/apt
# - npm cache: /root/.npm
# - pip cache: /root/.cache/pip
# - go cache: /root/go/pkg/mod
```

**Dockerfile doesn't update:**
```bash
# Force rebuild
ccy --rebuild

# Check if Dockerfile actually changed
git status .claude/ccy/Dockerfile
```

#### Comparison: --custom vs --custom-docker

| Feature | `--custom` | `--custom-docker` |
|---------|------------|-------------------|
| **Speed** | Fast (template selection) | Slower (investigation + planning) |
| **Guidance** | Basic | Comprehensive AI planning |
| **Investigation** | None | Deep project analysis |
| **Questions** | None | Targeted, contextual |
| **Proposals** | None | Feature list with reasoning |
| **Validation** | None | Syntax + logic checks |
| **Education** | Minimal | Extensive comments |
| **Best for** | Known tech stack | Unknown needs, learning |

**When to use `--custom`:**
- You know exactly what you need
- Using a common tech stack (Ansible, Go)
- Want quick setup

**When to use `--custom-docker`:**
- Unsure what tools are needed
- Complex/multi-language project
- Want to learn best practices
- Need optimized configuration

## Choosing the Right Technology

### Use LXC when you need:
- ✅ Complete isolated Linux system
- ✅ Multiple running services (systemd)
- ✅ Custom network configuration
- ✅ Testing system-level changes
- ✅ Long-running environments

### Use Docker when you need:
- ✅ Application/service deployment
- ✅ CI/CD pipelines
- ✅ Microservices architecture
- ✅ Reproducible production environments
- ✅ Container orchestration (Kubernetes)

### Use Distrobox when you need:
- ✅ Interactive development shells
- ✅ Access to different distro packages
- ✅ GUI applications from other distros
- ✅ Tool compatibility (Ubuntu-only tools)
- ✅ Clean development environments
- ✅ Seamless host integration

## Real-World Examples

### Example 1: Web Application Development
```bash
# LXC: Full isolated staging environment
sudo lxc-create -n staging -t download -- -d ubuntu -r jammy -a amd64
sudo lxc-start -n staging
ssh ubuntu@<staging-ip>
# Configure web server, database, services...

# Docker: Run individual services
docker-compose up -d  # Start postgres, redis, nginx

# Distrobox: Development tools
distrobox enter nodejs-dev
npm install && npm run dev  # Browser opens on host desktop
```

### Example 2: Browser Automation Testing
```bash
# Bad: Install Playwright on Fedora (pollutes host, may break)
npm install playwright  # ❌ System library conflicts, desktop pollution

# Good: Use CCY with built-in agent-browser
cd ~/Projects/my-project
ccy  # ✅ Docker-based, isolated, headed browser mode
# agent-browser is pre-installed — ask Claude to navigate, screenshot, test
# Browser windows appear on your desktop via Wayland forwarding
```

### Example 3: Multi-Distro Testing
```bash
# LXC: Test deployment scripts
sudo lxc-create -n ubuntu-test -t download
sudo lxc-create -n debian-test -t download
# Test installation on both

# Distrobox: Test development setup
distrobox create --name ubuntu-dev --image ubuntu:22.04
distrobox create --name arch-dev --image archlinux:latest
# Quickly test if your scripts work on both
```

## Performance Comparison

| Aspect | LXC | Docker | Distrobox |
|--------|-----|--------|-----------|
| **Startup Time** | ~5-10s | ~1-3s | <1s |
| **Memory Overhead** | ~50-100MB | ~10-50MB | ~5-20MB |
| **Storage Overhead** | ~500MB-1GB | ~100-500MB | ~100-500MB |
| **CPU Overhead** | Minimal | Minimal | Minimal |
| **I/O Performance** | Near-native | Near-native | Native (shared FS) |

## Security Considerations

### LXC
- Full system containers require careful security configuration
- SELinux in permissive mode (configured by playbook)
- Insecure SSH key for convenience (not for production)
- Containers can be privileged or unprivileged

### Docker
- Rootless mode configured (enhanced security)
- User namespace isolation
- Limited host access by default
- No privileged operations without explicit flags

### Distrobox
- Shares home directory (access to all your files)
- Uses host network (no isolation)
- Same UID inside and outside (by design)
- Trade-off: convenience over isolation
- **Not for untrusted code** - use Docker/LXC instead

## Maintenance

### LXC Maintenance
```bash
# Update container
sudo lxc-attach -n mycontainer -- apt update && apt upgrade -y

# Backup container
sudo lxc-copy -n mycontainer -N mycontainer-backup

# View all containers
lxc-ls  # Uses lxc-bash alias (sudo lxc-ls -f)
```

### LXC Shell Helpers (lxc-bash)

This project uses [lxc-bash](https://github.com/LongTermSupport/lxc-bash) for enhanced LXC command-line experience. It's automatically cloned and configured by `play-lxc-install-config.yml`.

**What it provides:**

| Command | Description |
|---------|-------------|
| `lxc-ls` | List all containers with status (alias for `sudo lxc-ls -f`) |
| `lxc-start <name>` | Start a stopped container (with tab completion) |
| `lxc-stop <name>` | Stop a running container (with tab completion) |
| `lxc-attach <name>` | Attach to container, auto-starting if stopped |
| `lxc-shutdown <name>` | Gracefully shutdown container via poweroff |
| `lxc-info <name>` | Show container info |
| `lxc-freeze <name>` | Freeze a running container |
| `lxc-unfreeze <name>` | Unfreeze a frozen container |

**Tab completion:**
All commands have intelligent tab completion that filters by container state:
- `lxc-start` only shows STOPPED containers
- `lxc-stop` only shows RUNNING containers
- `lxc-freeze` only shows RUNNING containers
- `lxc-unfreeze` only shows FROZEN containers

**Location:** Cloned to `~/Projects/lxc-bash/` and sourced from `~/.bashrc`

### Docker Maintenance
```bash
# Update images
docker pull image:latest

# Clean up unused resources
docker system prune -a

# View disk usage
docker system df
```

### Distrobox Maintenance
```bash
# Update container
distrobox enter mycontainer
sudo apt update && sudo apt upgrade -y
exit

# Rebuild container (fresh start)
distrobox rm mycontainer
distrobox create --name mycontainer --image ubuntu:22.04

# Upgrade distrobox itself
sudo dnf upgrade distrobox
```

## Troubleshooting

### LXC Issues
**Container won't get IP address:**
```bash
sudo systemctl restart lxc
sudo firewall-cmd --zone=trusted --change-interface=lxcbr0 --permanent
sudo firewall-cmd --reload
```

**SSH connection fails:**
```bash
# Check SSH key
ls ~/.ssh/id_lxc
# Check SSH config
grep -A 5 "10.0.*.*" ~/.ssh/config
```

### Docker Issues
**Permission denied:**
```bash
# Check rootless service
systemctl --user status docker
systemctl --user restart docker
```

**Out of space:**
```bash
docker system prune -a
docker volume prune
```

### Distrobox Issues
**Container won't start:**
```bash
# Check podman/docker
podman ps -a
# or
docker ps -a

# Recreate container
distrobox rm mycontainer
distrobox create --name mycontainer --image ubuntu:22.04
```

**GUI apps don't work:**
```bash
# Check X11 forwarding
echo $DISPLAY  # Should show something like :0 or :1

# Check Wayland
echo $WAYLAND_DISPLAY  # Should show wayland-0 or similar
```

## Further Reading

- [LXC Documentation](https://linuxcontainers.org/lxc/documentation/)
- [Docker Documentation](https://docs.docker.com/)
- [Distrobox GitHub](https://github.com/89luca89/distrobox)
- [Rootless Docker](https://docs.docker.com/engine/security/rootless/)
- [Project LXC Scripts](https://github.com/LongTermSupport/lxc-bash)
