#!/usr/bin/env bash

## Setup
set -e
set -u
set -o pipefail
standardIFS="$IFS"
IFS=$'\n\t'

## Assertions
if [[ "$(whoami)" == "root" ]];
then
  printf "\n\n ERROR - please do not run this as root\n\nSimply run as your normal user\n\n"
  exit 1
fi

## Functions

title(){
  printf "\n$1\n"
}

completed(){
  printf "\nDone...\n"
}

confirm(){
  local msg="$1"
  local yn=n
  while [[ "$yn" != "y" ]]; do
    echo
    read -sp "$msg (y/n)" -n 1 yn
  done
  printf "\n\n$msg confirmed\n\n"
}

promptForValue(){
  local item v yn
  item="$1"
  yn=n
  while [[ "$yn" != "y" ]]; do
    echo 1>&2
    read -p "Please enter your $item: " v
    echo 1>&2
    read -sp "Confirm correct value for $item is $v (y/n) " -n 1 yn
  done
  echo "$v"
}

## Process

echo "

Process Starting

You will be asked for sudo password

"
title "Installing Dependencies with DNF"
sudo dnf -y install \
  git \
  python3 \
  python3-pip \
  grubby
completed

title "Updating Grub Configs for Cgroups"
sudo grubby --update-kernel=ALL --args="systemd.unified_cgroup_hierarchy=0"
completed

title "Installing Ansible with Pip"
pip3 install \
  ansible \
  jmespath
completed

title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
if [[ ! -f ~/.ssh/id ]]; then
  while true; do
    read -s -p "Password: " password
    echo
    read -s -p "Password (confirm): " password2
    echo
    [ "$password" = "$password2" ] && break
    echo "Passwords not matched, please try again"
  done
  ssh-keygen -t ed25519 -f ~/.ssh/id -P "$password"
else
  echo " - found existing key"
fi
completed

title "Installing Github CLI"
sudo dnf install 'dnf-command(config-manager)'
sudo dnf config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo
sudo dnf install gh
completed

title "Configuring Github CLI (https://cli.github.com/)

We're now going to log into Github - you will need to authenticate with your browser
"
echo 'export GH_HOST="github.com"' >> ~/.bashrc
gh auth login
completed

title "Adding SSH Key to Github"
gh ssh-key add ~/.ssh/id.pub --title="Added by fedora-desktop setup script on $(date +%Y-%m-%d)" --type=authentication
completed

title "Creating Projects directory"
mkdir -p ~/Projects
completed

title "Cloning Fedora Desktop Repo"
cd ~/Projects
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  git clone git@github.com:LongTermSupport/fedora-desktop.git
fi
completed


if [[ ! -f ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml ]]; then
  title "Collecting required configs"
  echo "Your user login (probably $(whoami))"
  user_login="$(promptForValue user_login)"

  echo "Your full name"
  user_name="$(promptForValue user_full_name)"

  echo "Your email address"
  user_email="$(promptForValue user_email)"
  completed

  title "Updating ansible localhost config in environment/localhost/host_vars/localhost.yml"
  cat <<EOF > ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
user_login: "$user_login"
user_name: "$user_name"
user_email: "$user_email"
EOF
  completed
fi

title "Ensuring repo is up to date"
cd ~/Projects/fedora-desktop
git pull
completed

title "Now running Ansible to complete configuration"
# checking for passwordless sudo
if sudo -n true; then
  ./playbooks/playbook-main.yml
else
  ./playbooks/playbook-main.yml --ask-become-pass
fi
completed

title "And now your system needs to be rebooted"
confirm "Happy to Reboot ?"
sudo reboot now
