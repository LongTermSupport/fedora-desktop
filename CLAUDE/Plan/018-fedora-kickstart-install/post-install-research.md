# Fedora Kickstart to Ansible Post-Install Automation Pipeline

## Research Document

This document contains actionable research for chaining a Fedora Kickstart installation
into a full post-install automation pipeline that runs the `fedora-desktop` Ansible
playbooks from git.

---

## Table of Contents

1. [Kickstart %post: --nochroot vs chroot](#1-kickstart-post-nochroot-vs-chroot)
2. [Running Ansible from %post](#2-running-ansible-from-post)
3. [Firstboot / systemd Oneshot Services](#3-firstboot--systemd-oneshot-services)
4. [GitHub SSH Keys and Repo Cloning in %post](#4-github-ssh-keys-and-repo-cloning-in-post)
5. [Ansible Vault in Automated Installs](#5-ansible-vault-in-automated-installs)
6. [User Session Setup](#6-user-session-setup)
7. [Flatpak, RPM Fusion, Third-Party Repos in %post](#7-flatpak-rpm-fusion-third-party-repos-in-post)
8. [Real-World Examples](#8-real-world-examples)
9. [Recommended Architecture](#9-recommended-architecture)
10. [Complete Reference Kickstart File](#10-complete-reference-kickstart-file)

---

## 1. Kickstart %post: --nochroot vs chroot

### How %post Execution Works

By default, `%post` scripts run **inside a chroot** rooted at the newly installed
system's filesystem. The installed system is mounted and the chroot makes it appear
as if you are running commands directly on the installed system.

With `--nochroot`, the script runs in the **Anaconda installer environment** instead.
The installed system's root filesystem is accessible at `/mnt/sysimage`.

### Key Differences

| Aspect | `%post` (chroot, default) | `%post --nochroot` |
|--------|--------------------------|-------------------|
| Root filesystem | `/` = installed system | `/` = installer env, installed system at `/mnt/sysimage` |
| Package manager | `dnf` runs against installed system | Cannot run `dnf` for installed system |
| Network | May need resolv.conf fix | Has installer network config |
| File access | Only installed system files | Both installer media and installed system |
| Use case | Install packages, configure system | Copy files from media, fix DNS |

### Critical DNS Resolution Issue

When using DHCP, the chrooted `%post` may not have a working `/etc/resolv.conf`.
The standard fix is a two-phase approach:

```
# Phase 1: Copy resolv.conf from installer to installed system
%post --nochroot --log=/mnt/sysimage/root/ks-post-nochroot.log
cp /etc/resolv.conf /mnt/sysimage/etc/resolv.conf
%end

# Phase 2: Now chrooted %post has working DNS
%post --log=/root/ks-post.log
dnf -y install git ansible
%end
```

### Multiple %post Sections

A kickstart file can have **multiple %post sections**, each with independent options.
Each must be closed with `%end`. They execute in the order they appear.

```
%post --nochroot --log=/mnt/sysimage/root/ks-nochroot.log
# Runs in installer environment
cp /etc/resolv.conf /mnt/sysimage/etc/resolv.conf
%end

%post --log=/root/ks-post-packages.log --erroronfail
# Runs chrooted - install packages
dnf -y install git ansible-core python3-pip
%end

%post --log=/root/ks-post-config.log
# Runs chrooted - configure system
systemctl enable my-firstboot.service
%end
```

### Available Options

| Option | Description |
|--------|------------|
| `--nochroot` | Run outside chroot (installer environment) |
| `--interpreter=/path/to/interp` | Use specific interpreter (e.g., `/usr/bin/python3`) |
| `--erroronfail` | Halt installation if script fails |
| `--log=/path/to/log` | Log script output to file |

### Best Practices for %post Logging and Error Handling

```
%post --erroronfail --log=/root/ks-post.log
set -euxo pipefail

# Your commands here
echo "Starting post-install configuration..."

%end
```

Using `set -euxo pipefail` provides:
- `e`: Exit on error
- `u`: Treat unset variables as errors
- `x`: Print each command before execution (essential for debugging via the log)
- `o pipefail`: Pipe failures are caught

The `--log` path is relative to the chroot context:
- With chroot (default): `--log=/root/ks-post.log` writes to installed system's `/root/`
- With `--nochroot`: `--log=/mnt/sysimage/root/ks-nochroot.log` to write to installed system

### Sources

- [Red Hat Enterprise Linux 9: Kickstart script file format reference](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-script-file-format-reference_rhel-installer)
- [Fedora 20 Installation Guide: Post-installation Script](https://jfearn.fedorapeople.org/fdocs/en-US/Fedora/20/html/Installation_Guide/s1-kickstart2-postinstallconfig.html)
- [GoLinuxCloud: Kickstart post install script examples](https://www.golinuxcloud.com/kickstart-post-install-script-examples-rhel-8/)
- [Pykickstart Documentation](https://pykickstart.readthedocs.io/en/latest/kickstart-docs.html)

---

## 2. Running Ansible from %post

### Can Ansible Run in %post?

**Yes**, but with significant caveats. There are two main approaches:

### Approach A: Install and Run Ansible Directly in %post

```
%post --erroronfail --log=/root/ks-post-ansible.log
set -euxo pipefail

# Install Ansible and dependencies
dnf -y install ansible-core python3-pip git

# Install required collections
ansible-galaxy collection install community.general ansible.posix

# Clone the repo
git clone https://github.com/LongTermSupport/fedora-desktop.git /root/fedora-desktop

# Run the playbook
cd /root/fedora-desktop
ansible-playbook playbooks/playbook-main.yml --connection=local

%end
```

### Gotchas with Running Ansible in %post (Chroot)

1. **No systemd**: systemd is not running in the chroot. Tasks using `systemd` module
   to start/enable services may behave unexpectedly. `systemctl enable` works (it
   creates symlinks), but `systemctl start` will fail.

2. **No D-Bus session**: dconf, gsettings, and anything requiring a D-Bus session bus
   will fail. GNOME settings cannot be applied via `gsettings` command.

3. **Limited network**: DNS may not work without the resolv.conf fix (see Section 1).

4. **No user sessions**: Tasks that need to run as a specific user in a login session
   will not have the full environment.

5. **ansible-galaxy network requirements**: `ansible-galaxy collection install` needs
   internet access to download from Galaxy. If DNS is broken, this fails silently or
   with cryptic errors.

6. **python3-libdnf5 dependency**: The `fedora-desktop` project uses `python3-libdnf5`
   for the Ansible dnf module. This must be installed before running playbooks.

7. **pipx won't work**: The current `run.bash` installs Ansible via `pipx`, which needs
   a full user environment. In %post, use `dnf -y install ansible-core` or
   `pip3 install ansible` instead.

8. **Ansible collections from DNF**: On Fedora, collections can be installed via DNF
   as RPMs, which avoids Galaxy network issues:
   ```bash
   dnf -y install ansible-collection-community-general ansible-collection-ansible-posix
   ```

### Approach B: Use ansible-pull (Preferred for Scalability)

`ansible-pull` inverts the normal push model. The target machine pulls its own
configuration from a git repository and runs it locally:

```bash
ansible-pull \
  -U https://github.com/LongTermSupport/fedora-desktop.git \
  -C F42 \
  -d /root/fedora-desktop \
  -i environment/localhost/hosts.yml \
  playbooks/playbook-main.yml
```

Options:
- `-U`: Repository URL (HTTPS for public repos, no SSH key needed)
- `-C`: Branch/checkout (e.g., `F42` for Fedora 42)
- `-d`: Directory to clone into
- `-i`: Inventory file (relative to repo root)

### Collection Installation Before ansible-pull

Collections must be installed before `ansible-pull` runs. Two approaches:

```bash
# Option 1: Install from Fedora repos (no Galaxy needed)
dnf -y install ansible-collection-community-general ansible-collection-ansible-posix

# Option 2: Install from Galaxy (needs network)
ansible-galaxy collection install community.general ansible.posix

# Option 3: Install from requirements.yml after cloning
git clone https://github.com/LongTermSupport/fedora-desktop.git /tmp/fedora-desktop
ansible-galaxy install -r /tmp/fedora-desktop/requirements.yml
```

### Recommendation for fedora-desktop Project

**Do not run the full Ansible playbook suite in %post.** Instead:

1. In %post: Install minimal packages, set up a firstboot systemd service
2. On first boot: The systemd service runs `run.bash` or a modified version

This avoids all the chroot limitations and runs in a real, fully booted system.

### Sources

- [Calgary RHCE: Ansible-pull and kickstart](https://calgaryrhce.ca/blog/2016/02/03/ansible-pull-and-kickstart-for-one-touch-server-provisioning/)
- [DEV Community: From zero to hero - Bootstrap with Ansible](https://dev.to/thbe/from-zero-to-hero-bootstrap-with-ansible-2ohi)
- [Fedora Discussion: Trying to speed up fedora installation](https://discussion.fedoraproject.org/t/trying-to-speed-up-fedora-installation-and-configuration-a-journey-towards-automation-with-kickstart-and-ansible/97022)
- [Ansible Documentation: ansible-pull](https://docs.ansible.com/ansible/latest/cli/ansible-pull.html)

---

## 3. Firstboot / systemd Oneshot Services

### Why Firstboot is Better Than %post for Complex Setup

The %post environment is fundamentally limited:
- No running systemd (PID 1 is Anaconda, not systemd)
- No D-Bus (no session bus, no system bus in useful state)
- No user sessions
- Chroot environment may have incomplete state
- Network may be unreliable

A systemd oneshot service runs on the **first real boot** of the installed system,
with full systemd, networking, D-Bus, and all services available.

### Pattern: systemd Oneshot Service with ConditionFirstBoot

```ini
# /etc/systemd/system/fedora-desktop-setup.service
[Unit]
Description=Fedora Desktop First Boot Configuration
Documentation=https://github.com/LongTermSupport/fedora-desktop
After=network-online.target
Wants=network-online.target
# Ensure graphical target is not yet reached
Before=display-manager.service

[Service]
Type=oneshot
RemainAfterExit=no
ExecStart=/usr/local/bin/fedora-desktop-firstboot.sh
TimeoutStartSec=3600
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
```

### Pattern: Self-Disabling Service (Alternative to ConditionFirstBoot)

`ConditionFirstBoot=yes` relies on `/etc/machine-id` state. A more reliable
approach is a self-disabling service that uses a marker file:

```ini
# /etc/systemd/system/fedora-desktop-setup.service
[Unit]
Description=Fedora Desktop First Boot Configuration
After=network-online.target
Wants=network-online.target
ConditionPathExists=!/var/lib/fedora-desktop-setup-complete

[Service]
Type=oneshot
RemainAfterExit=no
ExecStart=/usr/local/bin/fedora-desktop-firstboot.sh
ExecStartPost=/usr/bin/touch /var/lib/fedora-desktop-setup-complete
ExecStartPost=/usr/bin/systemctl disable fedora-desktop-setup.service
TimeoutStartSec=3600
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
```

The `ConditionPathExists=!` check means the service only runs if the marker file
does not exist. After successful completion, it creates the marker and disables itself.

### Firstboot Script Example

```bash
#!/bin/bash
# /usr/local/bin/fedora-desktop-firstboot.sh
set -euxo pipefail

LOG_FILE="/var/log/fedora-desktop-firstboot.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "=== Fedora Desktop First Boot Setup ==="
echo "Started at: $(date)"

# Wait for network
for i in $(seq 1 30); do
    if ping -c 1 github.com &>/dev/null; then
        echo "Network is available"
        break
    fi
    echo "Waiting for network... ($i/30)"
    sleep 2
done

# Read configuration from the provisioning data
SETUP_USER=$(cat /var/lib/fedora-desktop/setup-user 2>/dev/null || echo "")
SETUP_BRANCH=$(cat /var/lib/fedora-desktop/setup-branch 2>/dev/null || echo "F42")
SETUP_REPO=$(cat /var/lib/fedora-desktop/setup-repo 2>/dev/null || echo "https://github.com/LongTermSupport/fedora-desktop.git")

if [[ -z "$SETUP_USER" ]]; then
    echo "ERROR: No setup user configured. Skipping Ansible setup."
    echo "Manual setup required: run ~/Projects/fedora-desktop/run.bash"
    exit 1
fi

SETUP_DIR="/home/${SETUP_USER}/Projects/fedora-desktop"

# Install dependencies
dnf -y install \
    git \
    ansible-core \
    python3-pip \
    python3-libdnf5 \
    ansible-collection-community-general \
    ansible-collection-ansible-posix

# Clone repository as the user
su - "$SETUP_USER" -c "
    mkdir -p ~/Projects
    if [[ ! -d '$SETUP_DIR' ]]; then
        git clone -b '$SETUP_BRANCH' '$SETUP_REPO' '$SETUP_DIR'
    fi
"

# Install Ansible Galaxy requirements
su - "$SETUP_USER" -c "
    cd '$SETUP_DIR'
    ansible-galaxy install -r requirements.yml
"

# Run the main playbook
cd "$SETUP_DIR"
ansible-playbook playbooks/playbook-main.yml --connection=local

echo "=== First Boot Setup Complete ==="
echo "Completed at: $(date)"
```

### ConditionFirstBoot Gotcha

`ConditionFirstBoot=yes` checks whether `/etc/machine-id` was freshly initialized.
This works correctly after Kickstart installations because:
- Anaconda creates an empty or uninitialized `/etc/machine-id`
- On first boot, systemd initializes it and considers it a "first boot"
- After reboot, ConditionFirstBoot=yes no longer triggers

However, if the machine-id was already set during installation (e.g., in %post),
`ConditionFirstBoot=yes` may not trigger. The marker file approach is safer.

### Comparison: %post vs Firstboot Service

| Aspect | %post | Firstboot Service |
|--------|-------|-------------------|
| systemd available | No | Yes |
| D-Bus available | No | Yes |
| Network reliability | Questionable (DNS issues) | Full network stack |
| User sessions | No | Can run as user |
| Service management | Can only enable, not start | Full start/stop/enable |
| dconf/gsettings | Must use file-based approach | Full gsettings available |
| Flatpak | May have D-Bus issues | Works fully |
| Time to install | Adds to Anaconda install time | Adds to first boot time |
| User visibility | Hidden during install | Can show progress on console |
| Failure handling | May leave system in bad state | System is bootable, can retry |

### Sources

- [Undrblog: Adding a first boot task via systemd](https://www.undrground.org/2021/01/25/adding-a-single-run-task-via-systemd/)
- [Red Hat Blog: systemd's oneshot service type](https://www.redhat.com/en/blog/systemd-oneshot-service)
- [systemd man page: systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
- [GitHub: systemd-firstboot issue #8268](https://github.com/systemd/systemd/issues/8268)
- [TheForeman: RFC - Systemd first boot service](https://community.theforeman.org/t/rfc-systemd-first-boot-service-for-host-provisioning/29892)

---

## 4. GitHub SSH Keys and Repo Cloning in %post

### The Problem

The `fedora-desktop` project is currently cloned via SSH (`git@github.com:...`) in
`run.bash`. SSH requires:
1. An SSH key pair on the machine
2. The public key registered with GitHub
3. GitHub host keys in `known_hosts`
4. Interactive `gh auth login` for key upload

None of these are available during Kickstart %post.

### Solution: Use HTTPS for Public Repos

Since the `fedora-desktop` repo is **public**, HTTPS cloning requires no
authentication:

```bash
# No SSH key needed for public repos
git clone https://github.com/LongTermSupport/fedora-desktop.git

# Specify branch
git clone -b F42 https://github.com/LongTermSupport/fedora-desktop.git
```

This works perfectly in both %post and firstboot contexts.

### SSH Key Setup as a Separate Step

SSH key generation and GitHub authentication should remain as a post-boot,
interactive step (as in the current `run.bash`). The pipeline becomes:

1. **Kickstart %post / Firstboot**: Clone via HTTPS, run Ansible playbooks
2. **User interactive session**: Run SSH key setup, `gh auth login`, switch
   remote to SSH if desired

### For Private Repos (Future Consideration)

If the repo were private, options include:

1. **GitHub Personal Access Token (PAT)**: Pass via kernel cmdline or embed in
   kickstart (not recommended for security).
   ```bash
   git clone https://<PAT>@github.com/LongTermSupport/fedora-desktop.git
   ```

2. **Deploy Key**: Pre-generate an SSH key, embed the private key in the kickstart
   (security risk), and add the public key as a GitHub deploy key.

3. **Temporary Token**: Use a short-lived token passed via kernel parameter.

For a public repo, none of this complexity is needed.

### Handling ansible-pull with HTTPS

`ansible-pull` works natively with HTTPS URLs:

```bash
ansible-pull \
    -U https://github.com/LongTermSupport/fedora-desktop.git \
    -C F42 \
    -d /tmp/fedora-desktop \
    playbooks/playbook-main.yml
```

### Sources

- [Jeff Geerling: Cloning private GitHub repositories with Ansible](https://www.jeffgeerling.com/blog/2018/cloning-private-github-repositories-ansible-on-remote-server-through-ssh)
- [Linux Handbook: Clone Git Repository with Ansible](https://linuxhandbook.com/clone-git-ansible/)

---

## 5. Ansible Vault in Automated Installs

### The Challenge

The `fedora-desktop` project uses Ansible Vault to encrypt sensitive data in
`environment/localhost/host_vars/localhost.yml`. The vault password is stored
in `vault-pass.secret` (gitignored). In an automated pipeline, this password
must be provided somehow.

### Current Vault Configuration (from ansible.cfg)

```ini
ask_vault_pass = False
vault_password_file=./vault-pass.secret
vault_identity=localhost
vault_id_match=true
```

### Strategy 1: Defer Vault to Interactive Session (Recommended)

The simplest approach: **do not use vault-encrypted data during automated setup**.

Analysis of the vault-encrypted variables in `localhost.yml`:
- `lastfm_api_key` / `lastfm_api_secret`: Not needed for core system setup
- `user_login`, `user_name`, `user_email`: These are **not** encrypted

The core playbooks (preflight, basic configs, packages) do not require vault
secrets. The vault-encrypted data is only needed for specific optional
configurations.

**Implementation**:
1. Split playbooks into "vault-free" and "vault-required" groups
2. Run vault-free playbooks in automated pipeline
3. Run vault-required playbooks interactively after first login

### Strategy 2: Vault Password via Kernel Command Line

Pass the vault password as a kernel parameter during boot:

```
# Boot parameter
inst.ks=https://example.com/ks.cfg vault_pass=MySecretPassword
```

In `%pre` or `%post`, extract it:

```bash
%pre --log=/tmp/ks-pre.log
# Extract vault password from kernel cmdline
VAULT_PASS=""
for param in $(cat /proc/cmdline); do
    case "$param" in
        vault_pass=*)
            VAULT_PASS="${param#vault_pass=}"
            ;;
    esac
done

if [[ -n "$VAULT_PASS" ]]; then
    # Write to a file that %post can access
    echo "$VAULT_PASS" > /tmp/vault-pass.txt
fi
%end

%post --nochroot
# Copy vault password to installed system
if [[ -f /tmp/vault-pass.txt ]]; then
    cp /tmp/vault-pass.txt /mnt/sysimage/root/vault-pass.txt
    chmod 600 /mnt/sysimage/root/vault-pass.txt
fi
%end

%post --log=/root/ks-post-ansible.log
# Use the vault password
if [[ -f /root/vault-pass.txt ]]; then
    cp /root/vault-pass.txt /path/to/fedora-desktop/vault-pass.secret
    chmod 600 /path/to/fedora-desktop/vault-pass.secret
    # Clean up
    rm -f /root/vault-pass.txt
fi
%end
```

**Security concern**: Kernel parameters are visible in `/proc/cmdline` and may
be logged. This is acceptable for local installations but not for network-visible
setups.

### Strategy 3: Vault Password from USB Drive

```bash
%post --nochroot --log=/mnt/sysimage/root/ks-vault.log
# Look for vault password on USB drive with specific label
VAULT_DEVICE=$(blkid -L "VAULT_KEY" 2>/dev/null || true)
if [[ -n "$VAULT_DEVICE" ]]; then
    mkdir -p /tmp/vault_mount
    mount "$VAULT_DEVICE" /tmp/vault_mount
    if [[ -f /tmp/vault_mount/vault-pass.secret ]]; then
        cp /tmp/vault_mount/vault-pass.secret /mnt/sysimage/root/vault-pass.secret
        chmod 600 /mnt/sysimage/root/vault-pass.secret
    fi
    umount /tmp/vault_mount
fi
%end
```

### Strategy 4: Generate New Vault Password Automatically

If the vault-encrypted values can be re-entered later, generate a new vault
password during automated install:

```bash
%post --log=/root/ks-post-vault.log
# Generate a new vault password
openssl rand -base64 32 > /path/to/fedora-desktop/vault-pass.secret
chmod 600 /path/to/fedora-desktop/vault-pass.secret

# The host_vars file will need to be re-encrypted with this new password
# or created without encrypted values initially
%end
```

### Strategy 5: Environment Variable

Ansible can read the vault password from an environment variable via a script:

```bash
# /usr/local/bin/vault-pass-from-env.sh
#!/bin/bash
echo "$ANSIBLE_VAULT_PASSWORD"
```

Then in ansible.cfg:
```ini
vault_password_file=/usr/local/bin/vault-pass-from-env.sh
```

### Recommendation for fedora-desktop

**Use Strategy 1 (Defer) combined with Strategy 4 (Auto-generate)**:

1. During automated install, create `localhost.yml` with only unencrypted values
2. Auto-generate a vault password for future use
3. Skip playbooks that require vault secrets
4. After first interactive login, user runs a script to add vault-encrypted values

### Sources

- [Ansible Documentation: Managing vault passwords](https://docs.ansible.com/projects/ansible/latest/vault_guide/vault_managing_passwords.html)
- [AutomateSQL: How to Automate Ansible Vault with a Password File](https://www.automatesql.com/blog/ansible-vault-password-file)
- [AutomateSQL: Using Ansible Vault with Environment Variables](https://www.automatesql.com/blog/using-ansible-vault-with-environment-variables)
- [Ansible Documentation: Encrypting content with Ansible Vault](https://docs.ansible.com/projects/ansible/latest/vault_guide/vault_encrypting_content.html)

---

## 6. User Session Setup

### The Problem

Some Ansible tasks in `fedora-desktop` need to:
- Run as a specific user (not root)
- Set dconf/gsettings values (requires D-Bus session)
- Configure user-level systemd services
- Set up dotfiles in the user's home directory

### Running as User in %post

In `%post`, you can use `su` or `runuser` to execute commands as a specific user:

```bash
%post --log=/root/ks-post-user.log
# Create user first (if not done by kickstart user command)
# useradd -m -G wheel joseph

# Run commands as the user
runuser -l joseph -c 'mkdir -p ~/Projects'
runuser -l joseph -c 'git clone https://github.com/example/repo.git ~/Projects/repo'
%end
```

In a firstboot service, similar approach:

```bash
su - "$SETUP_USER" -c "
    cd ~/Projects/fedora-desktop
    ansible-playbook playbooks/some-playbook.yml
"
```

### dconf/gsettings Without a Session (System-Wide Defaults)

`gsettings` requires a D-Bus session bus, which does not exist in %post or in
a systemd service running before the user logs in. The solution is to use
**dconf system databases** which are file-based and do not need D-Bus.

#### Step 1: Create dconf Profile

```bash
# /etc/dconf/profile/user
user-db:user
system-db:local
```

This tells dconf to check the user's database first, then fall back to the
system-wide "local" database.

#### Step 2: Create System-Wide Defaults

```bash
# /etc/dconf/db/local.d/01-desktop-settings
[org/gnome/desktop/interface]
clock-show-date=true
clock-show-weekday=true
gtk-theme='Adwaita-dark'
color-scheme='prefer-dark'

[org/gnome/desktop/wm/preferences]
button-layout='appmenu:minimize,maximize,close'

[org/gnome/desktop/peripherals/touchpad]
tap-to-click=true

[org/gnome/shell]
favorite-apps=['org.gnome.Nautilus.desktop', 'org.gnome.Terminal.desktop', 'firefox.desktop']
```

#### Step 3: Create Locks (Prevent User Override, Optional)

```bash
# /etc/dconf/db/local.d/locks/01-mandatory-settings
/org/gnome/desktop/interface/clock-show-date
```

#### Step 4: Update dconf Database

```bash
dconf update
```

This compiles the keyfiles into a binary database. **This must be run after
any changes to files in `/etc/dconf/db/`.**

#### Complete %post Example for GNOME Settings

```bash
%post --log=/root/ks-post-dconf.log
set -euxo pipefail

# Create dconf profile
mkdir -p /etc/dconf/profile
cat > /etc/dconf/profile/user << 'DCONF_PROFILE'
user-db:user
system-db:local
DCONF_PROFILE

# Create system-wide defaults
mkdir -p /etc/dconf/db/local.d
cat > /etc/dconf/db/local.d/01-fedora-desktop << 'DCONF_SETTINGS'
[org/gnome/desktop/interface]
clock-show-date=true
color-scheme='prefer-dark'

[org/gnome/desktop/wm/preferences]
button-layout='appmenu:minimize,maximize,close'

[org/gnome/shell]
favorite-apps=['org.gnome.Nautilus.desktop', 'org.gnome.Terminal.desktop', 'firefox.desktop']
DCONF_SETTINGS

# Compile the database
dconf update

%end
```

### User-Level systemd Services

User-level systemd services (`--user` scope) require a user session. These
cannot be enabled or started in %post. Options:

1. **Enable lingering**: `loginctl enable-linger USERNAME` allows user services
   to run without a login session, but this still requires systemd to be running.

2. **Create symlinks manually**: Instead of `systemctl --user enable service`,
   create the symlink directly:
   ```bash
   mkdir -p /home/joseph/.config/systemd/user/default.target.wants
   ln -s /usr/lib/systemd/user/service.service \
       /home/joseph/.config/systemd/user/default.target.wants/service.service
   ```

3. **Defer to first login**: Use an autostart desktop entry or a script in
   `~/.config/autostart/` to complete user-level setup on first login.

### Ansible become_user in Automated Context

When running Ansible as root (as in firstboot), `become_user` works:

```yaml
- name: Configure user dotfiles
  become: true
  become_user: "{{ user_login }}"
  copy:
    src: "{{ root_dir }}/files/home/user/.bashrc"
    dest: "/home/{{ user_login }}/.bashrc"
```

However, tasks that need a full user session (like `gsettings` commands) will
still fail. Use the dconf file-based approach instead.

### Sources

- [GNOME Admin Guide: Manage user and system settings with dconf](https://help.gnome.org/admin/system-admin-guide/stable/dconf.html.en)
- [Red Hat RHEL 8: Configuring GNOME at low level](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/using_the_desktop_environment_in_rhel_8/configuring-gnome-at-low-level_using-the-desktop-environment-in-rhel-8)
- [GNOME Wiki: dconf System Administrators](https://wiki.gnome.org/Projects/dconf/SystemAdministrators)
- [Kickstart mailing list: Setting gconf/dconf keys in kickstart](https://kickstart-list.redhat.narkive.com/8Y3chG1x/setting-gconf-dconf-keys-in-kickstart)

---

## 7. Flatpak, RPM Fusion, Third-Party Repos in %post

### RPM Fusion in %post

RPM Fusion can be installed in `%post` since it only requires `dnf install` of
an RPM from a URL. This works as long as network and DNS are available:

```bash
%post --erroronfail --log=/root/ks-post-repos.log
set -euxo pipefail

FEDORA_VERSION=$(rpm -E %{fedora})

# Install RPM Fusion Free and Nonfree
dnf -y install \
    "https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-${FEDORA_VERSION}.noarch.rpm" \
    "https://mirrors.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-${FEDORA_VERSION}.noarch.rpm"

# Now install packages from RPM Fusion
dnf -y install \
    akmod-nvidia \
    ffmpeg \
    gstreamer1-plugins-bad-freeworld

%end
```

**Note**: For NVIDIA drivers (akmods), the kernel module needs to be built.
In %post this may not complete properly since the running kernel is the
installer's kernel, not the installed system's kernel. NVIDIA driver installation
is better handled at firstboot or as a separate post-boot step.

### Flatpak and Flathub in %post

Flatpak setup involves two parts: enabling the Flathub remote and installing
applications.

#### Enabling Flathub Remote (Works in %post)

```bash
%post --log=/root/ks-post-flatpak.log
set -euxo pipefail

# Ensure flatpak is installed
dnf -y install flatpak

# Add Flathub remote (system-wide)
flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo

%end
```

#### Installing Flatpak Applications (Problematic in %post)

Flatpak application installation in %post has issues:
- Requires D-Bus system bus for some operations
- Large downloads (runtimes) significantly increase install time
- May fail due to missing D-Bus or polkit sessions
- The `flatpak install` command may hang waiting for authentication

**Recommendation**: Install Flatpak apps in the firstboot service or defer to
the existing optional playbook `play-install-flatpaks.yml`.

```bash
# In firstboot service (after boot, with full systemd):
flatpak install -y flathub org.mozilla.firefox
flatpak install -y flathub com.spotify.Client
```

### Third-Party DNF Repos in %post

Adding third-party repos works well in %post:

```bash
%post --erroronfail --log=/root/ks-post-repos.log
set -euxo pipefail

# GitHub CLI repository
dnf -y install 'dnf-command(config-manager)'
dnf config-manager addrepo --from-repofile=https://cli.github.com/packages/rpm/gh-cli.repo

# Docker CE repository
dnf config-manager addrepo --from-repofile=https://download.docker.com/linux/fedora/docker-ce.repo

# Install packages from the new repos
dnf -y install gh docker-ce docker-ce-cli containerd.io

%end
```

### Summary: What Works Where

| Component | %post | Firstboot | Notes |
|-----------|-------|-----------|-------|
| RPM Fusion repos | Yes | Yes | URL-based RPM install works |
| RPM Fusion packages | Mostly | Yes | Akmods (NVIDIA) better at firstboot |
| Flathub remote | Yes | Yes | Simple remote-add works |
| Flatpak apps | Risky | Yes | D-Bus issues in %post |
| Third-party DNF repos | Yes | Yes | Works well in both |
| Docker/Podman | Partial | Yes | Can install, cannot start in %post |
| NVIDIA drivers | No | Yes | Needs running kernel for akmods |

### Sources

- [RPM Fusion FAQ](https://rpmfusion.org/FAQ)
- [Flathub: Fedora Setup](https://flathub.org/en/setup/Fedora)
- [Fedora Discussion: Flatpak during kickstart in air-gapped network](https://discussion.fedoraproject.org/t/flatpak-during-kickstart-in-air-gapped-network/154274/1)
- [Fedora Discussion: Install Flatpaks from Kickstart](https://discussion.fedoraproject.org/t/install-flatpaks-from-kickstart-with-livecd-creator/133095)

---

## 8. Real-World Examples

### Example 1: fedora-homeserver (dschier-wtd)

A well-maintained GitHub project that uses Kickstart for initial Fedora installation
followed by Ansible for configuration.

**Repository**: https://github.com/dschier-wtd/fedora-homeserver

**Approach**:
- Kickstart handles: disk partitioning, base packages, user creation, network
- Ansible handles: all post-install configuration (run separately after first boot)
- Two kickstart files: `base_ks.cfg` (minimal) and `full_ks.cfg` (customized)
- Usage: Add `inst.ks=https://your.url/ks.cfg` to kernel boot parameters

**Key insight**: They intentionally keep Kickstart minimal and do not try to run
Ansible from within %post. Ansible is run manually after the machine boots.

### Example 2: Calgary RHCE ansible-pull Pattern

**Source**: https://calgaryrhce.ca/blog/2016/02/03/ansible-pull-and-kickstart-for-one-touch-server-provisioning/

**Architecture**:
1. Kickstart installs base OS
2. %post creates a systemd service for first boot
3. First boot service runs ansible-pull to configure the system
4. Service disables itself after successful run

**Key components**:

Kickstart %post section:
```bash
%post
# Install ansible
yum install -y epel-release
yum install -y ansible git

# Create the firstboot script
cat > /usr/local/bin/ansible-config-me.sh << 'SCRIPT'
#!/bin/bash
runuser -l ansible -c 'ansible-pull \
    -C master \
    -d /home/ansible/deploy \
    -i /home/ansible/hosts \
    -U git@gitlab.example.com:user/ansible.git \
    --accept-host-key \
    --purge >> /home/ansible/run.log 2>&1'
systemctl disable ansible-config-me.service
SCRIPT
chmod +x /usr/local/bin/ansible-config-me.sh

# Create systemd service
cat > /etc/systemd/system/ansible-config-me.service << 'SERVICE'
[Unit]
Description=Run ansible-pull at first boot
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/ansible-config-me.sh
Type=oneshot

[Install]
WantedBy=multi-user.target
SERVICE

systemctl enable ansible-config-me.service
%end
```

### Example 3: SMARTRACTECHNOLOGY ansible-pull-systemd

**Repository**: https://github.com/SMARTRACTECHNOLOGY/ansible-pull-systemd

Provides a systemd service and timer for running ansible-pull on a schedule.
While designed for ongoing configuration management, the pattern is useful for
first-boot scenarios too.

### Example 4: Fedora Discussion Community Approach

**Source**: https://discussion.fedoraproject.org/t/trying-to-speed-up-fedora-installation-and-configuration-a-journey-towards-automation-with-kickstart-and-ansible/97022

Community discussion about combining Kickstart and Ansible for Fedora desktop
automation. Key takeaways:
- Kickstart for disk layout, boot config, and base packages
- Ansible for everything else (run after first boot)
- The community recommends against complex %post scripts
- ansible-pull is the preferred integration pattern

### Example 5: Kickstart Getting Started (while-true-do.io)

**Source**: https://blog.while-true-do.io/kickstart-getting-started/

Provides a clean, modern example of Kickstart configuration for Fedora with
explanations of each section. Useful as a template for the base kickstart file.

### Sources

- [GitHub: dschier-wtd/fedora-homeserver](https://github.com/dschier-wtd/fedora-homeserver)
- [Calgary RHCE: Ansible-pull and kickstart](https://calgaryrhce.ca/blog/2016/02/03/ansible-pull-and-kickstart-for-one-touch-server-provisioning/)
- [GitHub: SMARTRACTECHNOLOGY/ansible-pull-systemd](https://github.com/SMARTRACTECHNOLOGY/ansible-pull-systemd)
- [Fedora Discussion: Kickstart and Ansible automation](https://discussion.fedoraproject.org/t/trying-to-speed-up-fedora-installation-and-configuration-a-journey-towards-automation-with-kickstart-and-ansible/97022)
- [while-true-do.io: Kickstart Getting Started](https://blog.while-true-do.io/kickstart-getting-started/)

---

## 9. Recommended Architecture

Based on all the research above, here is the recommended architecture for the
`fedora-desktop` project's Kickstart-to-Ansible pipeline.

### Three-Phase Architecture

```
Phase 1: Kickstart           Phase 2: Firstboot              Phase 3: Interactive
(Anaconda Installer)         (systemd oneshot)                (User Session)

+-------------------+        +-------------------------+     +-------------------------+
| - Disk partioning |        | - Clone repo via HTTPS  |     | - SSH key generation    |
| - Base packages   |        | - Install Ansible deps  |     | - gh auth login         |
| - User creation   |        | - Run vault-free        |     | - Vault secret entry    |
| - Network config  |------->|   playbooks             |---->| - Optional playbooks    |
| - Copy resolv.conf|        | - RPM Fusion setup      |     | - Flatpak apps          |
| - Install ansible |        | - Core system config    |     | - GNOME extensions      |
| - Create firstboot|        | - Self-disable service  |     | - Hardware-specific     |
|   systemd service |        +-------------------------+     +-------------------------+
+-------------------+
```

### Phase 1: Kickstart (%post)

**Goal**: Get the system bootable with the minimum needed for Phase 2.

What to do in %post:
1. Fix DNS (copy resolv.conf via --nochroot)
2. Install: `git`, `ansible-core`, `python3-libdnf5`, `python3-pip`
3. Install Ansible collections from Fedora repos (avoid Galaxy network dependency)
4. Write provisioning configuration files (user, branch, repo URL)
5. Create the firstboot systemd service and script
6. Enable the firstboot service

What NOT to do in %post:
- Run Ansible playbooks
- Install Flatpak apps
- Configure dconf/gsettings via gsettings command
- Start services
- Build kernel modules (akmods)
- Set up SSH keys or GitHub authentication
- Handle vault secrets

### Phase 2: Firstboot (systemd oneshot service)

**Goal**: Run the core Ansible playbooks in a fully booted environment.

The firstboot service should:
1. Wait for network availability
2. Clone the repo via HTTPS (as the target user)
3. Generate a vault password file (if not provided)
4. Create a minimal `localhost.yml` without encrypted values
5. Run vault-free playbooks (preflight, basic-configs, packages, etc.)
6. Set up dconf system defaults for GNOME
7. Install RPM Fusion and third-party repos
8. Mark completion and disable itself

### Phase 3: Interactive (User Session)

**Goal**: Complete setup that requires user interaction.

After the user's first login:
1. SSH key generation (requires passphrase input)
2. GitHub CLI authentication (requires browser)
3. Vault secret entry (API keys, etc.)
4. Optional playbooks (Flatpak apps, Docker, etc.)
5. Hardware-specific configurations

This can be triggered by:
- A desktop notification prompting the user
- An autostart script that checks for completion
- The user manually running a setup script

### Passing Data Between Phases

Data flows between phases via files on the installed filesystem:

```
/var/lib/fedora-desktop/
    setup-user              # Username for the setup
    setup-branch            # Git branch (e.g., "F42")
    setup-repo              # Repository URL
    setup-complete          # Marker file (created after Phase 2)

/home/<user>/Projects/fedora-desktop/
    vault-pass.secret       # Auto-generated vault password
    environment/localhost/host_vars/localhost.yml  # User config (no vault)
```

### Passing Data from Kickstart to %post

Use kernel command line parameters and a `%pre` section:

```
# Boot parameters:
inst.ks=https://example.com/ks.cfg ks.user=joseph ks.branch=F42
```

Extract in %pre, write to files, copy to installed system in %post --nochroot,
then read in %post (chroot) and firstboot service.

---

## 10. Complete Reference Kickstart File

This is a complete, actionable Kickstart file template for the `fedora-desktop`
project. It implements the three-phase architecture described above.

```
#version=F42
# Fedora Desktop Kickstart Configuration
# For use with: fedora-desktop Ansible automation project
# Repository: https://github.com/LongTermSupport/fedora-desktop

#
# ============================================================
# INSTALLATION CONFIGURATION
# ============================================================
#

# Use graphical installer (or text for headless)
graphical

# Installation source
# For network install, uncomment and set mirror:
# url --mirrorlist="https://mirrors.fedoraproject.org/mirrorlist?repo=fedora-$releasever&arch=$basearch"

# Keyboard and language
keyboard --xlayouts='gb'
lang en_GB.UTF-8

# Timezone
timezone Europe/London --utc

# Network configuration
network --bootproto=dhcp --device=link --activate --hostname=fedora-desktop

# Root password (lock root account)
rootpw --lock

# Create user (change as needed)
user --name=joseph --groups=wheel --gecos="Joseph" --password=changeme --plaintext

# SELinux
selinux --enforcing

# Firewall
firewall --enabled --service=ssh

# Disk partitioning
# WARNING: This will ERASE ALL DATA on the target disk
zerombr
clearpart --all --initlabel
autopart --type=lvm --encrypted --passphrase=changeme

# Bootloader
bootloader --location=mbr --append="rhgb quiet"

# Reboot after installation
reboot --eject

#
# ============================================================
# PACKAGES
# ============================================================
#

%packages
@^workstation-product-environment
@development-tools
vim-enhanced
wget
curl
git
bash-completion
htop
jq
openssl
grubby
python3
python3-pip
python3-libdnf5
ansible-core
ansible-collection-community-general
ansible-collection-ansible-posix
%end

#
# ============================================================
# PRE-INSTALLATION SCRIPT
# ============================================================
#

%pre --log=/tmp/ks-pre.log
#!/bin/bash
set -euxo pipefail

# Parse kernel command line for custom parameters
SETUP_USER=""
SETUP_BRANCH="F42"
SETUP_REPO="https://github.com/LongTermSupport/fedora-desktop.git"
VAULT_PASS=""

for param in $(cat /proc/cmdline); do
    case "$param" in
        ks.user=*)
            SETUP_USER="${param#ks.user=}"
            ;;
        ks.branch=*)
            SETUP_BRANCH="${param#ks.branch=}"
            ;;
        ks.repo=*)
            SETUP_REPO="${param#ks.repo=}"
            ;;
        ks.vault_pass=*)
            VAULT_PASS="${param#ks.vault_pass=}"
            ;;
    esac
done

# Write configuration for later phases
mkdir -p /tmp/fedora-desktop-config
echo "$SETUP_USER" > /tmp/fedora-desktop-config/setup-user
echo "$SETUP_BRANCH" > /tmp/fedora-desktop-config/setup-branch
echo "$SETUP_REPO" > /tmp/fedora-desktop-config/setup-repo
if [[ -n "$VAULT_PASS" ]]; then
    echo "$VAULT_PASS" > /tmp/fedora-desktop-config/vault-pass
fi

echo "Pre-install config written:"
echo "  User: $SETUP_USER"
echo "  Branch: $SETUP_BRANCH"
echo "  Repo: $SETUP_REPO"
echo "  Vault: $(if [[ -n '$VAULT_PASS' ]]; then echo 'provided'; else echo 'not provided'; fi)"

%end

#
# ============================================================
# POST-INSTALLATION: Phase 1a - Copy files from installer env
# ============================================================
#

%post --nochroot --log=/mnt/sysimage/root/ks-post-nochroot.log
#!/bin/bash
set -euxo pipefail

echo "=== Post-install (nochroot): Copying configuration files ==="

# Fix DNS resolution for chrooted %post sections
cp /etc/resolv.conf /mnt/sysimage/etc/resolv.conf

# Copy pre-install configuration to installed system
mkdir -p /mnt/sysimage/var/lib/fedora-desktop
if [[ -d /tmp/fedora-desktop-config ]]; then
    cp -r /tmp/fedora-desktop-config/* /mnt/sysimage/var/lib/fedora-desktop/
    chmod 700 /mnt/sysimage/var/lib/fedora-desktop
    chmod 600 /mnt/sysimage/var/lib/fedora-desktop/*
fi

echo "=== Post-install (nochroot): Complete ==="

%end

#
# ============================================================
# POST-INSTALLATION: Phase 1b - System configuration (chrooted)
# ============================================================
#

%post --erroronfail --log=/root/ks-post-system.log
#!/bin/bash
set -euxo pipefail

echo "=== Post-install (chroot): System configuration ==="

# Read configuration
SETUP_USER=$(cat /var/lib/fedora-desktop/setup-user 2>/dev/null || echo "")
SETUP_BRANCH=$(cat /var/lib/fedora-desktop/setup-branch 2>/dev/null || echo "F42")
SETUP_REPO=$(cat /var/lib/fedora-desktop/setup-repo 2>/dev/null || echo "https://github.com/LongTermSupport/fedora-desktop.git")

# Configure passwordless sudo for the setup user
if [[ -n "$SETUP_USER" ]]; then
    echo "${SETUP_USER} ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/99-fedora-desktop-setup
    chmod 440 /etc/sudoers.d/99-fedora-desktop-setup
fi

# Enable DNF parallel downloads
echo "max_parallel_downloads=10" >> /etc/dnf/dnf.conf

# -------------------------------------------------------
# Create the firstboot script
# -------------------------------------------------------
cat > /usr/local/bin/fedora-desktop-firstboot.sh << 'FIRSTBOOT_SCRIPT'
#!/bin/bash
set -euxo pipefail

LOG_FILE="/var/log/fedora-desktop-firstboot.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "============================================"
echo "  Fedora Desktop First Boot Configuration"
echo "============================================"
echo "Started at: $(date)"
echo ""

# Read configuration
SETUP_USER=$(cat /var/lib/fedora-desktop/setup-user 2>/dev/null || echo "")
SETUP_BRANCH=$(cat /var/lib/fedora-desktop/setup-branch 2>/dev/null || echo "F42")
SETUP_REPO=$(cat /var/lib/fedora-desktop/setup-repo 2>/dev/null || echo "https://github.com/LongTermSupport/fedora-desktop.git")

if [[ -z "$SETUP_USER" ]]; then
    echo "WARNING: No setup user configured."
    echo "Please run setup manually after login."
    exit 0
fi

echo "Configuration:"
echo "  User: $SETUP_USER"
echo "  Branch: $SETUP_BRANCH"
echo "  Repo: $SETUP_REPO"
echo ""

# Wait for network to be fully available
echo "Waiting for network..."
for i in $(seq 1 60); do
    if curl -sf --max-time 5 https://github.com > /dev/null 2>&1; then
        echo "Network is available (attempt $i)"
        break
    fi
    if [[ $i -eq 60 ]]; then
        echo "ERROR: Network not available after 120 seconds"
        exit 1
    fi
    sleep 2
done

SETUP_DIR="/home/${SETUP_USER}/Projects/fedora-desktop"

# Clone repository as the user
echo "Cloning repository..."
su - "$SETUP_USER" -c "
    mkdir -p ~/Projects
    if [[ ! -d '$SETUP_DIR' ]]; then
        git clone -b '$SETUP_BRANCH' '$SETUP_REPO' '$SETUP_DIR'
    else
        cd '$SETUP_DIR'
        git pull
    fi
"

# Set up vault password (generate if not provided)
if [[ -f /var/lib/fedora-desktop/vault-pass ]]; then
    cp /var/lib/fedora-desktop/vault-pass "$SETUP_DIR/vault-pass.secret"
else
    openssl rand -base64 32 > "$SETUP_DIR/vault-pass.secret"
fi
chown "$SETUP_USER:$SETUP_USER" "$SETUP_DIR/vault-pass.secret"
chmod 600 "$SETUP_DIR/vault-pass.secret"

# Create minimal localhost.yml if it does not exist
if [[ ! -f "$SETUP_DIR/environment/localhost/host_vars/localhost.yml" ]]; then
    FULL_NAME=$(getent passwd "$SETUP_USER" | cut -d: -f5 | cut -d, -f1)
    cat > "$SETUP_DIR/environment/localhost/host_vars/localhost.yml" << HOSTVARS
user_login: "$SETUP_USER"
user_name: "${FULL_NAME:-$SETUP_USER}"
user_email: ""
HOSTVARS
    chown "$SETUP_USER:$SETUP_USER" "$SETUP_DIR/environment/localhost/host_vars/localhost.yml"
fi

# Install Ansible Galaxy requirements
echo "Installing Ansible Galaxy requirements..."
su - "$SETUP_USER" -c "
    cd '$SETUP_DIR'
    ansible-galaxy install -r requirements.yml || true
"

# Run the main playbook
echo "Running main Ansible playbook..."
cd "$SETUP_DIR"
ansible-playbook playbooks/playbook-main.yml \
    --connection=local \
    || echo "WARNING: Some playbook tasks may have failed. Check the log."

echo ""
echo "============================================"
echo "  First Boot Configuration Complete"
echo "============================================"
echo "Completed at: $(date)"
echo ""
echo "Next steps:"
echo "  1. Log in as $SETUP_USER"
echo "  2. Run: cd ~/Projects/fedora-desktop"
echo "  3. Run: ./run.bash (for SSH keys and GitHub auth)"
echo "  4. Run optional playbooks as needed"

# Clean up sensitive data
rm -f /var/lib/fedora-desktop/vault-pass

FIRSTBOOT_SCRIPT
chmod +x /usr/local/bin/fedora-desktop-firstboot.sh

# -------------------------------------------------------
# Create the systemd service
# -------------------------------------------------------
cat > /etc/systemd/system/fedora-desktop-setup.service << 'SYSTEMD_SERVICE'
[Unit]
Description=Fedora Desktop First Boot Configuration
Documentation=https://github.com/LongTermSupport/fedora-desktop
After=network-online.target
Wants=network-online.target
# Run before the display manager so config is applied before first login
Before=display-manager.service
ConditionPathExists=!/var/lib/fedora-desktop-setup-complete

[Service]
Type=oneshot
RemainAfterExit=no
ExecStart=/usr/local/bin/fedora-desktop-firstboot.sh
ExecStartPost=/usr/bin/touch /var/lib/fedora-desktop-setup-complete
ExecStartPost=/usr/bin/systemctl disable fedora-desktop-setup.service
TimeoutStartSec=3600
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
SYSTEMD_SERVICE

# Enable the firstboot service
systemctl enable fedora-desktop-setup.service

echo "=== Post-install (chroot): System configuration complete ==="
echo "Firstboot service has been created and enabled."

%end

#
# ============================================================
# POST-INSTALLATION: Phase 1c - GNOME defaults (chrooted)
# ============================================================
#

%post --log=/root/ks-post-dconf.log
#!/bin/bash
set -euxo pipefail

echo "=== Post-install (chroot): Setting GNOME defaults ==="

# Create dconf profile
mkdir -p /etc/dconf/profile
cat > /etc/dconf/profile/user << 'EOF'
user-db:user
system-db:local
EOF

# Create system-wide GNOME defaults
mkdir -p /etc/dconf/db/local.d
cat > /etc/dconf/db/local.d/01-fedora-desktop << 'EOF'
[org/gnome/desktop/interface]
clock-show-date=true
clock-show-weekday=true
color-scheme='prefer-dark'

[org/gnome/desktop/wm/preferences]
button-layout='appmenu:minimize,maximize,close'

[org/gnome/desktop/peripherals/touchpad]
tap-to-click=true

[org/gnome/desktop/input-sources]
xkb-options=['terminate:ctrl_alt_bksp']
EOF

# Compile the dconf database
dconf update

echo "=== Post-install (chroot): GNOME defaults configured ==="

%end

#
# ============================================================
# ERROR HANDLING
# ============================================================
#

%onerror
#!/bin/bash
echo "=== KICKSTART ERROR ==="
echo "An error occurred during the Kickstart installation."
echo "Check /tmp/ks-pre.log and /root/ks-post-*.log for details."
echo "======================="
%end
```

### Boot Parameters for This Kickstart

When booting the installer, add these kernel parameters:

```
inst.ks=https://example.com/ks.cfg ks.user=joseph ks.branch=F42
```

Optional parameters:
```
ks.repo=https://github.com/LongTermSupport/fedora-desktop.git
ks.vault_pass=MySecretVaultPassword
```

### Adapting for the fedora-desktop Project

To integrate this with the existing `fedora-desktop` project:

1. **Add the kickstart file** to the repository (e.g., `kickstart/ks.cfg`)

2. **Create a "headless-compatible" playbook** that skips tasks requiring:
   - SSH keys
   - GitHub authentication
   - Vault-encrypted variables
   - Interactive user input

3. **Modify `run.bash`** to detect if Phase 2 has already completed and skip
   redundant steps (check for `/var/lib/fedora-desktop-setup-complete`)

4. **Create a Phase 3 script** (e.g., `setup-interactive.bash`) that handles:
   - SSH key generation
   - `gh auth login`
   - Vault password entry and secret encryption
   - Optional playbook menu

### Testing the Kickstart

1. **Virtual Machine**: Use virt-manager or QEMU to test with a Fedora ISO
2. **Boot parameter**: Add `inst.ks=file:///path/to/ks.cfg` if on local media
3. **HTTP server**: Serve the kickstart via `python3 -m http.server` for network testing
4. **Validation**: Use `ksvalidator` from the `pykickstart` package:
   ```bash
   dnf install pykickstart
   ksvalidator kickstart/ks.cfg
   ```

---

## Appendix A: Passing Data Between Kickstart Phases

### Method 1: Kernel Command Line Parameters (Recommended)

Parameters added to the boot command line are available in `/proc/cmdline`
in all phases (%pre, %post --nochroot, %post).

```
# Boot parameters
inst.ks=https://example.com/ks.cfg ks.user=joseph ks.branch=F42
```

```bash
# Parsing in any script section
for param in $(cat /proc/cmdline); do
    case "$param" in
        ks.user=*) SETUP_USER="${param#ks.user=}" ;;
        ks.branch=*) SETUP_BRANCH="${param#ks.branch=}" ;;
    esac
done
```

Pros: Simple, universally accessible
Cons: Visible in logs, not suitable for long secrets

### Method 2: Files Written in %pre (Used Above)

Write files in %pre to `/tmp/`, copy to installed system in %post --nochroot,
read in %post (chroot) and in the firstboot service.

### Method 3: Kickstart File Templating

Generate the kickstart file dynamically (e.g., with Jinja2 or sed) before
serving it, embedding values directly:

```bash
# Generate customized kickstart
sed -e "s/%%USERNAME%%/joseph/g" \
    -e "s/%%BRANCH%%/F42/g" \
    ks-template.cfg > ks.cfg
```

---

## Appendix B: Ansible Playbook Compatibility Matrix

Tasks from the `fedora-desktop` playbooks and their compatibility with each phase:

| Playbook | %post | Firstboot | Interactive | Notes |
|----------|-------|-----------|-------------|-------|
| play-AA-preflight-sanity | Yes | Yes | Yes | Version checks only |
| play-basic-configs | Partial | Yes | Yes | sysctl works, service start needs systemd |
| play-systemd-user-tweaks | No | Partial | Yes | Needs user session for --user scope |
| play-nvm-install | No | Yes | Yes | Needs network, user env |
| play-git-configure-and-tools | No | Partial | Yes | Git config works, SSH setup needs interaction |
| play-github-cli-multi | No | No | Yes | Needs interactive auth |
| play-git-hooks-security | No | Yes | Yes | File operations only |
| play-lxc-install-config | No | Yes | Yes | Needs systemd for service |
| play-ms-fonts | Partial | Yes | Yes | Package install works |
| play-rpm-fusion | Yes | Yes | Yes | URL-based RPM install |
| play-toolbox-install | No | Yes | Yes | Needs podman/systemd |
| play-docker | No | Yes | Yes | Needs systemd for service |
| play-python | No | Yes | Yes | Needs network, user env |
| play-claude-code | No | Yes | Yes | Needs network |
| play-podman | No | Yes | Yes | Needs systemd |
| play-install-flatpaks | No | Partial | Yes | Flatpak install needs D-Bus |

---

## Appendix C: Troubleshooting

### Common %post Issues

1. **DNS resolution failure**: Ensure resolv.conf is copied in --nochroot section
2. **dnf fails in %post**: Check that packages are available; use `--allowerasing`
   if conflicts occur
3. **ansible-galaxy timeout**: Use Fedora RPM packages instead of Galaxy
4. **Service won't start**: Expected in %post; use `systemctl enable` only
5. **User not found**: Ensure the `user` kickstart command runs before %post

### Debugging Kickstart

```bash
# View kickstart logs after installation
cat /root/ks-post-system.log
cat /root/ks-post-nochroot.log
cat /root/ks-post-dconf.log

# View firstboot log
journalctl -u fedora-desktop-setup.service
cat /var/log/fedora-desktop-firstboot.log

# Verify firstboot service status
systemctl status fedora-desktop-setup.service
ls -la /var/lib/fedora-desktop-setup-complete

# Check dconf database
dconf dump /org/gnome/
```

### The inst.nokill Boot Parameter

If `--erroronfail` triggers, add `inst.nokill` to boot parameters to keep the
installer shell alive for debugging:

```
inst.ks=https://example.com/ks.cfg inst.nokill
```

This drops you to a shell where you can inspect logs and the chroot environment.

### Validating the Kickstart File

```bash
# Install the validator
sudo dnf install pykickstart

# Validate syntax
ksvalidator ks.cfg

# Check for deprecated commands
ksvalidator --version F42 ks.cfg
```
