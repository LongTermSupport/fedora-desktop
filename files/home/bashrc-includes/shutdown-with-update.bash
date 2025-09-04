#!/bin/bash
# Shutdown with Update - Updates firmware and packages before shutdown

alias shutdown-with-update='sudo bash -c "echo \"Starting firmware updates...\" && fwupdmgr refresh --force && fwupdmgr update -y && echo \"Starting system package updates...\" && dnf -y upgrade && echo \"Updates complete. Shutting down...\" && shutdown -h now"'