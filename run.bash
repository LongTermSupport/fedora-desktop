#!/usr/bin/env bash

## Setup
set -euo pipefail
standardIFS="$IFS"
IFS=$'\n\t'

## Assertions
## Functions

title(){
  printf "\n$1\n"
}

completed(){
  printf "\nDone...\n"
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
  python3-pip
completed
  
title "Installing Ansible with Pip"
pip3 install ansible
completed

title "Creating Projects directory"
mkdir -p ~/Projects
cd ~/Projects
completed

title "Cloning Fedora Desktop Repo"
git clone https://github.com/LongTermSupport/fedora-desktop.git
complete

cd fedora-desktop

cp environment/localhost/host_vars/localhost.yml.dist environment/localhost/host_vars/localhost.yml

echo "

New file created at cp environment/localhost/host_vars/localhost.yml.dist environment/localhost/host_vars/localhost.yml

You need to edit that file and replace all values with correct information

Then you need to run this command:

~/Projects/fedora-desktop/playbooks/playbook-main.yml

"
exit 0
