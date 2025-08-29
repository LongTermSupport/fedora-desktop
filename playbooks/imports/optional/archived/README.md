# Archived Playbooks

This directory contains playbooks that have been archived because they are no longer needed or have been superseded by built-in Fedora functionality.

## Archived Playbooks

### play-tlp-battery-optimisation.yml
- **Archived Date**: 2025-08-29
- **Reason**: Battery care and power management are now standard features in modern Fedora
- **Details**: 
  - Fedora 42+ includes automatic power management via power-profiles-daemon
  - GNOME Power Manager provides comprehensive battery optimization
  - TLP conflicts with the built-in power-profiles-daemon
  - The built-in solution provides better integration with the desktop environment
- **Alternative**: Use GNOME Settings > Power or `powerprofilesctl` command

## Notes

These playbooks are kept for historical reference but should not be used on current Fedora systems as they may conflict with built-in functionality or cause issues.