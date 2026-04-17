# DDEV Local Development Environment

DDEV is a Docker-based local development tool for PHP/CMS projects (Drupal, WordPress, Laravel, Magento, TYPO3, etc.). It provides per-project containers with a web server, database, and services — configured via a `.ddev/` directory in each project.

## Installation

```bash
ansible-playbook playbooks/imports/optional/common/play-ddev.yml
```

This installs:

- **mkcert** — local HTTPS certificate authority (from Fedora repos)
- **DDEV** — via the official yum repository at `pkg.ddev.com`

**Prerequisite:** Docker must be installed first:

```bash
ansible-playbook playbooks/imports/play-docker.yml
```

## Verify Installation

```bash
ddev version
mkcert -version
```

## Getting an EC Project Running Locally

### 1. Clone the Repository

```bash
cd ~/Projects
git clone git@github.com:Edmonds-Commerce-Limited/project-name.git
cd project-name
```

### 2. Check for DDEV Configuration

The repo should contain a `.ddev/` directory with a `config.yaml` — this defines the project type, PHP version, database, and services.

```bash
ls .ddev/config.yaml
```

If the `.ddev/` directory exists, the project is DDEV-ready.

### 3. Symlink the DDEV Environment Config (Magento/EC Projects)

EC projects include a `ddev.env.php` with database and cache settings for DDEV. Symlink it into place before starting:

```bash
ln -sf ddev.env.php app/etc/env.php
```

This replaces the production `env.php` with the DDEV-specific configuration (database host, credentials, cache backends, etc.).

**Forgot to do this before `ddev start`?** No problem — create the symlink and restart:

```bash
ln -sf ddev.env.php app/etc/env.php
ddev restart
ddev exec bin/magento cache:flush
```

### 4. Start DDEV

```bash
ddev start
```

This pulls and starts the containers (web server, database, etc.) based on `.ddev/config.yaml`. First run downloads images and takes longer.

### 5. Import the Database (if needed)

If the project needs a database dump:

```bash
# Import a SQL file
ddev import-db --file=path/to/database.sql.gz

# Or from an uncompressed file
ddev import-db --file=path/to/database.sql
```

Supported formats: `.sql`, `.sql.gz`, `.sql.bz2`, `.sql.xz`, `.zip`, `.tar`, `.tar.gz`, `.tgz`.

### 6. Access the Site

```bash
# Open in browser
ddev launch

# Or check the URL
ddev describe
```

DDEV provides HTTPS URLs with locally-trusted certificates (via mkcert).

## How DDEV Routing Works

No `/etc/hosts` changes needed. DDEV uses three layers:

1. **Public wildcard DNS** — The `ddev.site` domain has DNS records that resolve `*.ddev.site` to `127.0.0.1` (localhost). Your project URL (e.g. `https://project-name.ddev.site`) already points to your machine.
2. **ddev-router** — A traefik reverse proxy container listens on ports 80/443 on localhost. It receives the request and routes it to the correct project container based on the hostname.
3. **mkcert** — Provides locally-trusted HTTPS certificates for `*.ddev.site`, so the browser trusts the connection without warnings.

**Request flow:** browser → DNS (`*.ddev.site` → `127.0.0.1`) → ddev-router (port 80/443) → project container.

## Common Commands

| Command          | Description                         |
| ---------------- | ----------------------------------- |
| `ddev start`     | Start project containers            |
| `ddev stop`      | Stop project containers             |
| `ddev restart`   | Restart containers                  |
| `ddev describe`  | Show project info and URLs          |
| `ddev launch`    | Open site in browser                |
| `ddev ssh`       | SSH into the web container          |
| `ddev import-db` | Import a database dump              |
| `ddev export-db` | Export the database                 |
| `ddev composer`  | Run Composer inside the container   |
| `ddev mysql`     | Open MySQL/MariaDB client           |
| `ddev logs`      | View container logs                 |
| `ddev poweroff`  | Stop all DDEV projects and networks |
| `ddev list`      | List all DDEV projects              |
| `ddev config`    | Configure DDEV for a new project    |

## Setting Up a New Project (No .ddev/ Yet)

If a project doesn't have DDEV configuration:

```bash
cd ~/Projects/project-name
ddev config
```

Follow the prompts to set project type, PHP version, and docroot. This creates the `.ddev/` directory. Commit it to the repo so others can use it.

## Troubleshooting

### Docker Not Running

```bash
# Check Docker status
systemctl --user status docker --no-pager -l

# Start rootless Docker
systemctl --user start docker
```

### Port Conflicts

```bash
# See what's using the port
ddev describe
ss -tlnp | grep :80

# Change ports in .ddev/config.yaml:
# router_http_port: 8080
# router_https_port: 8443
```

### Reset a Project

```bash
# Remove containers and database (keeps files)
ddev delete -O

# Start fresh
ddev start
```

### Logs

```bash
# Web server logs
ddev logs

# Database logs
ddev logs -s db
```

## Upgrading DDEV

Since DDEV is installed via yum repo, upgrade with:

```bash
sudo dnf upgrade ddev
```

## Further Reading

- [DDEV Documentation](https://ddev.readthedocs.io/)
- [DDEV GitHub](https://github.com/ddev/ddev)
