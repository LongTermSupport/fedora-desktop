#!/usr/bin/env bash

## Setup
set -euo pipefail
standardIFS="$IFS"
IFS=$'\n\t'

## Assertions
## Functions


## Process

echo "

Process Starting

You will be asked for sudo password

"

sudo dnf -y install \
  git \
  python3 \
  python3-pip
  
pip3 install ansible

mkdir ~/Projects
cd ~/Projects

git clone git@github.com:LongTermSupport/fedora-desktop.git

cd fedora-desktop
