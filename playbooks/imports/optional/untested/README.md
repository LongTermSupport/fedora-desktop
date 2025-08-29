# Untested Playbooks

This directory contains playbooks that have not been tested on the current Fedora version.

These playbooks may:
- Use outdated configuration formats
- Reference paths that have changed
- Depend on packages that are no longer available
- Contain settings that conflict with current system defaults

## Testing Required

Before using any playbook in this directory:
1. Review the playbook contents carefully
2. Test in a non-production environment first
3. Verify all paths and configurations are correct for your Fedora version
4. Check that required packages are available

## Current Untested Playbooks

- **play-bluetooth-headphones-fix.yml**: Updated for WirePlumber 0.5+ but needs testing with actual Bluetooth hardware
- **play-hd-audio.yml**: PipeWire configuration format may have changed, needs verification

## Moving to Tested

Once a playbook has been verified to work correctly:
1. Move it back to the appropriate directory (common/hardware-specific/experimental)
2. Update this README to remove it from the untested list
3. Document any changes that were required for compatibility