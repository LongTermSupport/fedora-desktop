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

### Playwright Distrobox (Automated)

This project provides an automated Playwright testing environment via distrobox:

```bash
# Install and configure
ansible-playbook playbooks/imports/optional/common/play-distrobox-playwright.yml
```

This creates a shared `playwright-tests` container that:
- Uses Ubuntu 22.04 (Playwright officially supported)
- Installs Node.js LTS v20
- Installs Chromium, Firefox, and WebKit browsers
- Provides GUI support for visible browser testing
- Can be used by any project in `~/Projects/`

**Usage:**
```bash
# From any project directory
cd ~/Projects/my-project/tests

# Option 1: Enter Playwright container directly
distrobox enter playwright-tests
npm install
npm test

# Option 2: Use Claude Code for browser automation (recommended)
ccy-browser
# Claude Code launches inside the container
# Can help write/debug Playwright tests interactively
# Browsers appear on your desktop
```

**Claude Code Browser Automation Mode:**

The `ccy-browser` command launches Claude Code inside the Playwright container for AI-assisted browser automation:

```bash
cd ~/Projects/my-project/tests
ccy-browser

# Inside Claude Code:
# - Ask Claude to write Playwright tests
# - Debug failing browser tests interactively
# - Explore web applications with live browser feedback
# - All browsers visible on your desktop
```

**Benefits:**
- Zero pollution of Fedora desktop (all test tools in container)
- ~400MB browsers shared across all projects
- Each project maintains its own `node_modules/` and `package.json`
- Different Playwright versions per project (no conflicts)
- One-time setup via Ansible
- AI-assisted test development with `ccy-browser`

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
# Bad: Install Playwright on Fedora (unsupported, may break)
npm install playwright  # ❌ System library conflicts

# Good: Use Playwright distrobox
distrobox enter playwright-tests
npm install playwright
npm test  # ✅ Works perfectly, browsers on desktop
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
sudo lxc-ls -f
```

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
