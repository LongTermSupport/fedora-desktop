#!/usr/bin/env python3
"""Automatic Kernel Minor Version Management

Ensures that kernels from the two most recent MINOR versions are retained:
- Latest minor version: All patches managed by DNF installonly_limit
- Previous minor version: Lock the highest patch version to prevent removal

Example: If you have 6.16.12, 6.17.4, 6.17.5 installed:
  - 6.17.x is latest minor, managed normally by DNF
  - 6.16.12 is locked to prevent removal (dock compatibility)
  - When 6.18.x arrives, 6.17.x becomes locked, 6.16.x unlocked
"""

import argparse
import logging
import os
import re
import subprocess
import sys
from collections import defaultdict
from dataclasses import dataclass


@dataclass
class KernelVersion:
    """Represents a kernel version with semantic versioning."""

    full_version: str
    major: int
    minor: int
    patch: int
    release: str

    @classmethod
    def parse(cls, version_string: str) -> "KernelVersion":
        """Parse kernel version string.

        Expected format: X.Y.Z-RELEASE (e.g., 6.17.5-200.fc42.x86_64)
        """
        match = re.match(r"^(\d+)\.(\d+)\.(\d+)-(.+)$", version_string)
        if not match:
            raise ValueError(f"Invalid kernel version format: {version_string}")

        major, minor, patch, release = match.groups()
        return cls(
            full_version=version_string,
            major=int(major),
            minor=int(minor),
            patch=int(patch),
            release=release,
        )

    @property
    def minor_version(self) -> str:
        """Return the minor version string (e.g., '6.17')."""
        return f"{self.major}.{self.minor}"

    def __lt__(self, other: "KernelVersion") -> bool:
        """Compare kernel versions for sorting."""
        return (self.major, self.minor, self.patch) < (other.major, other.minor, other.patch)

    def __eq__(self, other: object) -> bool:
        """Check if two kernel versions are equal."""
        if not isinstance(other, KernelVersion):
            return NotImplemented
        return self.full_version == other.full_version

    def __hash__(self) -> int:
        """Make KernelVersion hashable."""
        return hash(self.full_version)

    def __str__(self) -> str:
        """String representation."""
        return self.full_version


class KernelManager:
    """Manages kernel versionlock operations."""

    def __init__(self, notify_user: str | None = None, dry_run: bool = False):
        self.notify_user = notify_user
        self.dry_run = dry_run
        self.logger = self._setup_logging()

    def _setup_logging(self) -> logging.Logger:
        """Configure logging to both console and journal."""
        logger = logging.getLogger("kernel-version-manager")
        logger.setLevel(logging.INFO)

        # Console handler with colors
        console = logging.StreamHandler()
        console.setLevel(logging.INFO)
        formatter = logging.Formatter("%(levelname)s: %(message)s")
        console.setFormatter(formatter)
        logger.addHandler(console)

        # Journal handler (if available)
        try:
            from systemd import journal
            journal_handler = journal.JournalHandler(SYSLOG_IDENTIFIER="kernel-version-manager")
            journal_handler.setLevel(logging.INFO)
            logger.addHandler(journal_handler)
        except ImportError:
            pass  # systemd-python not available, skip journal logging

        return logger

    def run_command(self, cmd: list[str], check: bool = True) -> tuple[int, str, str]:
        """Run a command and return (returncode, stdout, stderr).

        Args:
            cmd: Command and arguments as list
            check: Raise exception on non-zero return code

        Returns:
            Tuple of (returncode, stdout, stderr)

        """
        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                check=check,
            )
            return result.returncode, result.stdout, result.stderr
        except subprocess.CalledProcessError as e:
            if check:
                raise
            return e.returncode, e.stdout, e.stderr

    def get_installed_kernels(self) -> list[KernelVersion]:
        """Get list of installed kernel versions."""
        returncode, stdout, stderr = self.run_command([
            "rpm", "-q", "kernel", "--queryformat", "%{VERSION}-%{RELEASE}\n",
        ])

        if returncode != 0:
            self.logger.error(f"Failed to query installed kernels: {stderr}")
            return []

        kernels = []
        for line in stdout.strip().split("\n"):
            line = line.strip()
            if not line:
                continue
            try:
                kernel = KernelVersion.parse(line)
                kernels.append(kernel)
            except ValueError as e:
                self.logger.warning(f"Skipping invalid kernel version: {e}")
                continue

        return sorted(kernels)

    def get_running_kernel(self) -> KernelVersion | None:
        """Get currently running kernel version."""
        returncode, stdout, _ = self.run_command(["uname", "-r"])
        if returncode != 0:
            return None

        version_str = stdout.strip().replace(".x86_64", "")
        try:
            return KernelVersion.parse(version_str)
        except ValueError:
            self.logger.warning(f"Could not parse running kernel version: {version_str}")
            return None

    def is_kernel_locked(self, kernel: KernelVersion) -> bool:
        """Check if a kernel version is locked."""
        returncode, stdout, _ = self.run_command(
            ["dnf", "versionlock", "list"],
            check=False,
        )

        if returncode != 0:
            return False

        lock_pattern = f"kernel-0:{kernel.full_version}"
        return lock_pattern in stdout

    def lock_kernel(self, kernel: KernelVersion) -> None:
        """Lock a kernel version to prevent removal."""
        if self.is_kernel_locked(kernel):
            self.logger.info(f"Already locked: {kernel}")
            return

        if self.dry_run:
            self.logger.info(f"[DRY-RUN] Would lock: {kernel}")
            return

        self.logger.info(f"Locking kernel: {kernel}")
        returncode, _, stderr = self.run_command(
            ["dnf", "versionlock", "add", f"kernel-0:{kernel.full_version}.x86_64"],
            check=False,
        )

        if returncode != 0:
            self.logger.error(f"Failed to lock {kernel}: {stderr}")

    def unlock_kernel(self, kernel: KernelVersion) -> None:
        """Unlock a kernel version to allow removal."""
        if not self.is_kernel_locked(kernel):
            self.logger.info(f"Already unlocked: {kernel}")
            return

        if self.dry_run:
            self.logger.info(f"[DRY-RUN] Would unlock: {kernel}")
            return

        self.logger.info(f"Unlocking kernel: {kernel}")
        returncode, _, stderr = self.run_command(
            ["dnf", "versionlock", "delete", f"kernel-0:{kernel.full_version}.x86_64"],
            check=False,
        )

        if returncode != 0:
            self.logger.error(f"Failed to unlock {kernel}: {stderr}")

    def notify_desktop(self, summary: str, body: str, urgency: str = "normal") -> None:
        """Send desktop notification via libnotify."""
        if not self.notify_user:
            return

        try:
            # Get user ID
            result = subprocess.run(
                ["id", "-u", self.notify_user],
                capture_output=True,
                text=True,
                check=True,
            )
            uid = result.stdout.strip()

            # Send notification as user
            env = {
                "DISPLAY": ":0",
                "DBUS_SESSION_BUS_ADDRESS": f"unix:path=/run/user/{uid}/bus",
            }
            subprocess.run(
                ["sudo", "-u", self.notify_user, "notify-send",
                 "-u", urgency, "-a", "Kernel Manager", summary, body],
                env=env,
                check=False,
            )
        except (subprocess.CalledProcessError, FileNotFoundError):
            pass  # Notification failed, not critical

    def group_by_minor_version(self, kernels: list[KernelVersion]) -> dict[str, list[KernelVersion]]:
        """Group kernels by minor version."""
        groups = defaultdict(list)
        for kernel in kernels:
            groups[kernel.minor_version].append(kernel)

        # Sort kernels within each group
        for minor in groups:
            groups[minor] = sorted(groups[minor])

        return dict(groups)

    def manage_kernels(self, min_kernel_count: int = 2) -> None:
        """Main kernel management logic.

        Args:
            min_kernel_count: Minimum number of kernels to keep (safety)

        """
        self.logger.info("Starting kernel version management")

        # Check versionlock availability
        returncode, _, _ = self.run_command(["dnf", "versionlock", "list"], check=False)
        if returncode != 0:
            self.logger.error("DNF versionlock plugin not available. Install python3-dnf-plugin-versionlock")
            sys.exit(1)

        # Get installed kernels
        kernels = self.get_installed_kernels()
        if not kernels:
            self.logger.error("No kernels found!")
            sys.exit(1)

        self.logger.info(f"Found {len(kernels)} installed kernel(s): {', '.join(str(k) for k in kernels)}")

        # Safety check
        if len(kernels) < min_kernel_count:
            self.logger.warning(f"Only {len(kernels)} kernel(s) installed. Skipping management (minimum: {min_kernel_count})")
            return

        # Get running kernel
        running_kernel = self.get_running_kernel()
        if running_kernel:
            self.logger.info(f"Currently running: {running_kernel}")

        # Group by minor version
        groups = self.group_by_minor_version(kernels)
        sorted_minors = sorted(groups.keys(), key=lambda v: tuple(map(int, v.split("."))))

        self.logger.info(f"Found {len(sorted_minors)} minor version(s): {', '.join(sorted_minors)}")

        # Identify latest and previous minor versions
        latest_minor = sorted_minors[-1]
        previous_minor = sorted_minors[-2] if len(sorted_minors) >= 2 else None

        self.logger.info(f"Latest minor version: {latest_minor}")
        if previous_minor:
            self.logger.info(f"Previous minor version: {previous_minor}")

        # Process each minor version
        locked_count = 0
        unlocked_count = 0
        old_minors = []

        for minor in sorted_minors:
            kernels_in_minor = groups[minor]
            latest_in_minor = kernels_in_minor[-1]  # Highest patch version

            self.logger.info(f"Processing minor {minor}: {', '.join(str(k) for k in kernels_in_minor)}")

            if minor == latest_minor:
                # Latest minor: unlock all (let DNF manage)
                self.logger.info("  → Latest minor version, unlocking all")
                for kernel in kernels_in_minor:
                    if running_kernel and kernel == running_kernel:
                        self.logger.info(f"    Skipping {kernel} (currently running)")
                        continue
                    self.unlock_kernel(kernel)
                    unlocked_count += 1

            elif minor == previous_minor:
                # Previous minor: lock highest patch version
                self.logger.info(f"  → Previous minor version, locking highest patch: {latest_in_minor}")
                self.lock_kernel(latest_in_minor)
                locked_count += 1

                # Unlock others in this minor
                for kernel in kernels_in_minor:
                    if kernel != latest_in_minor:
                        self.unlock_kernel(kernel)
                        unlocked_count += 1

            else:
                # Older minors: unlock all (candidates for removal)
                self.logger.info("  → Old minor version, unlocking all (available for removal)")
                old_minors.append(minor)
                for kernel in kernels_in_minor:
                    if running_kernel and kernel == running_kernel:
                        self.logger.info(f"    Skipping {kernel} (currently running)")
                        continue
                    self.unlock_kernel(kernel)
                    unlocked_count += 1

        self.logger.info(f"Kernel management complete: {locked_count} locked, {unlocked_count} unlocked")

        # Notify about old minors
        if old_minors:
            old_minor_list = ", ".join(old_minors)
            self.logger.warning(f"Old kernel minor versions available for removal: {old_minor_list}")
            self.notify_desktop(
                "Old Kernels Available for Removal",
                f"Kernel versions {old_minor_list} can be removed.\nRun: sudo dnf remove kernel-{old_minors[0]}.*",
                "normal",
            )

        # Show current versionlock status
        self.logger.info("Current versionlock status:")
        returncode, stdout, _ = self.run_command(["dnf", "versionlock", "list"], check=False)
        if returncode == 0 and stdout.strip():
            for line in stdout.strip().split("\n"):
                if "kernel" in line:
                    self.logger.info(f"  {line}")
        else:
            self.logger.info("  (no kernel locks)")


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description="Automatic Kernel Minor Version Management",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without making changes",
    )
    parser.add_argument(
        "--notify-user",
        type=str,
        default=os.environ.get("NOTIFY_USER"),
        help="Username for desktop notifications (default: $NOTIFY_USER)",
    )
    parser.add_argument(
        "--min-kernels",
        type=int,
        default=2,
        help="Minimum number of kernels to keep (default: 2)",
    )

    args = parser.parse_args()

    # Check for root privileges
    if os.geteuid() != 0:
        print("ERROR: This script must be run as root", file=sys.stderr)
        sys.exit(1)

    manager = KernelManager(notify_user=args.notify_user, dry_run=args.dry_run)
    manager.manage_kernels(min_kernel_count=args.min_kernels)


if __name__ == "__main__":
    main()
