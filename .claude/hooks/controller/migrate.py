#!/usr/bin/env python3
"""
Migration script for hooks front controller.

This script:
1. Backs up current hooks and settings to .bak files
2. Deploys new front controller configuration
3. Provides rollback capability

Usage:
    python3 migrate.py               # Show what would change (dry-run)
    python3 migrate.py --deploy      # Actually deploy
    python3 migrate.py --rollback    # Restore from .bak files
"""

import json
import os
import shutil
import sys
from datetime import datetime
from pathlib import Path

# Paths
HOOKS_DIR = Path(__file__).parent.parent
CONTROLLER_DIR = Path(__file__).parent
SETTINGS_FILE = HOOKS_DIR.parent / "settings.local.json"
BACKUP_DIR = CONTROLLER_DIR / "backups"


def log(msg, level="INFO"):
    """Print timestamped log message."""
    timestamp = datetime.now().strftime("%H:%M:%S")
    print(f"[{timestamp}] {level}: {msg}")


def backup_file(filepath):
    """Backup a file with .bak extension and timestamp."""
    if not filepath.exists():
        log(f"Skipping backup of non-existent file: {filepath}", "WARN")
        return None

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup_path = filepath.parent / f"{filepath.name}.bak.{timestamp}"

    shutil.copy2(filepath, backup_path)
    log(f"Backed up: {filepath.name} -> {backup_path.name}")
    return backup_path


def backup_hooks():
    """Backup all current hook files."""
    log("Backing up existing hook files...")

    # Find all .py hook files (excluding controller/)
    hook_files = []
    for hook_file in HOOKS_DIR.glob("*.py"):
        if hook_file.name == "__init__.py":
            continue
        hook_files.append(hook_file)

    backups = []
    for hook_file in hook_files:
        backup_path = backup_file(hook_file)
        if backup_path:
            backups.append((hook_file, backup_path))

    log(f"Backed up {len(backups)} hook files")
    return backups


def backup_settings():
    """Backup settings.local.json."""
    log("Backing up settings.local.json...")
    return backup_file(SETTINGS_FILE)


def create_new_settings():
    """Create new settings.local.json with front controller hooks."""
    log("Creating new settings configuration...")

    # Read existing settings
    existing = {}
    if SETTINGS_FILE.exists():
        with open(SETTINGS_FILE, 'r') as f:
            existing = json.load(f)

    # Start with existing settings
    new_settings = existing.copy()

    # Initialize hooks if not present
    if "hooks" not in new_settings:
        new_settings["hooks"] = {}

    # Replace ONLY PreToolUse with front controller
    # Preserve all other hook events (PostToolUse, UserPromptSubmit, SubagentStop)
    new_settings["hooks"]["PreToolUse"] = [
        {
            "hooks": [
                {
                    "type": "command",
                    "command": str(CONTROLLER_DIR / "pre_tool_use.py"),
                    "timeout": 60
                }
            ]
        }
    ]

    return new_settings


def show_diff(old_settings, new_settings):
    """Show what will change."""
    log("Configuration changes:")
    print("\n" + "="*60)
    print("OLD CONFIGURATION (settings.local.json):")
    print("="*60)
    if "hooks" in old_settings:
        print(json.dumps(old_settings["hooks"], indent=2))
    else:
        print("(no hooks configured)")

    print("\n" + "="*60)
    print("NEW CONFIGURATION (settings.local.json):")
    print("="*60)
    print(json.dumps(new_settings["hooks"], indent=2))
    print("="*60 + "\n")


def deploy(dry_run=True):
    """Deploy new front controller configuration."""
    if dry_run:
        log("DRY RUN - No files will be changed", "INFO")
    else:
        log("DEPLOYING - Files will be modified", "WARN")

    # Read current settings
    old_settings = {}
    if SETTINGS_FILE.exists():
        with open(SETTINGS_FILE, 'r') as f:
            old_settings = json.load(f)

    # Create new settings
    new_settings = create_new_settings()

    # Show diff
    show_diff(old_settings, new_settings)

    if dry_run:
        log("Dry run complete. Run with --deploy to apply changes.")
        return

    # Backup current state
    log("\n=== BACKUP PHASE ===")
    hook_backups = backup_hooks()
    settings_backup = backup_settings()

    # Deploy new configuration
    log("\n=== DEPLOYMENT PHASE ===")

    # Write new settings
    log(f"Writing new settings to {SETTINGS_FILE}")
    with open(SETTINGS_FILE, 'w') as f:
        json.dump(new_settings, f, indent=2)
        f.write('\n')  # Add trailing newline

    log("✓ New settings deployed")

    # Make controller scripts executable
    log("Setting executable permissions on controller scripts...")
    controller_scripts = [
        CONTROLLER_DIR / "pre_tool_use.py",
        CONTROLLER_DIR / "front_controller.py",
    ]

    for script in controller_scripts:
        if script.exists():
            os.chmod(script, 0o755)
            log(f"✓ Made executable: {script.name}")

    # Summary
    log("\n=== DEPLOYMENT COMPLETE ===")
    log(f"Backed up {len(hook_backups)} hook files")
    log(f"Settings backup: {settings_backup.name if settings_backup else 'N/A'}")
    log(f"New configuration: {SETTINGS_FILE}")
    log("\nTo rollback: python3 migrate.py --rollback")


def rollback(force=False):
    """Restore from most recent .bak files."""
    log("ROLLING BACK to previous configuration", "WARN")

    # Find most recent settings backup
    settings_backups = list(SETTINGS_FILE.parent.glob(f"{SETTINGS_FILE.name}.bak.*"))
    if not settings_backups:
        log("No settings backup found!", "ERROR")
        return False

    latest_settings_backup = max(settings_backups, key=lambda p: p.stat().st_mtime)

    # Find all hook backups
    hook_backups = list(HOOKS_DIR.glob("*.py.bak.*"))

    log(f"Found settings backup: {latest_settings_backup.name}")
    log(f"Found {len(hook_backups)} hook backups")

    if not force:
        # Confirm
        print("\nThis will:")
        print(f"  - Restore settings from {latest_settings_backup.name}")
        print(f"  - Restore {len(hook_backups)} hook files")
        print(f"  - Overwrite current settings.local.json")

        confirm = input("\nProceed with rollback? [y/N]: ")
        if confirm.lower() != 'y':
            log("Rollback cancelled")
            return False

    # Restore settings
    log(f"Restoring {SETTINGS_FILE.name}...")
    shutil.copy2(latest_settings_backup, SETTINGS_FILE)
    log("✓ Settings restored")

    # Restore hook files
    for backup in hook_backups:
        original = HOOKS_DIR / backup.name.split('.bak.')[0]
        log(f"Restoring {original.name}...")
        shutil.copy2(backup, original)

    log(f"✓ Restored {len(hook_backups)} hook files")
    log("\n=== ROLLBACK COMPLETE ===")
    return True


def main():
    """Main entry point."""
    args = sys.argv[1:]

    if "--rollback" in args:
        force = "--force" in args
        rollback(force=force)
    elif "--deploy" in args:
        deploy(dry_run=False)
    else:
        # Default: dry run
        deploy(dry_run=True)


if __name__ == '__main__':
    main()
