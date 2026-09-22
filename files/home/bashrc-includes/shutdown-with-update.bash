#!/bin/bash
# Shutdown with Update - Updates firmware and packages, warns every running ccy/cc
# session, counts down, then shuts down. reboot-with-update is the same script by
# another name, ending in a reboot (docs/ccy.md, "Sessions Survive a Reboot").

alias shutdown-with-update='sudo /usr/local/bin/shutdown-with-update'
alias reboot-with-update='sudo /usr/local/bin/reboot-with-update'