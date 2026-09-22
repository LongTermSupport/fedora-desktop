#!/bin/bash
# Shutdown with Update - Updates firmware and packages before shutdown.
# reboot-with-update is the same script by another name: updates, then warns every
# running ccy/cc session and reboots (docs/ccy.md, "Sessions Survive a Reboot").

alias shutdown-with-update='sudo /usr/local/bin/shutdown-with-update'
alias reboot-with-update='sudo /usr/local/bin/reboot-with-update'