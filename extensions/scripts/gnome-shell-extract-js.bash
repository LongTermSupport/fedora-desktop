#!/bin/bash
#
# Extract GNOME Shell JavaScript source from installed version
#
# The JS files are embedded in libshell-*.so as GResources.
# This script extracts them to ./untracked/gnome-shell/<version>/js-extracted/
# for local reference when developing extensions.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
UNTRACKED_DIR="$REPO_ROOT/untracked/gnome-shell"

GRESOURCE_FILE="/usr/lib64/gnome-shell/libshell-16.so"

# Get precise GNOME Shell version
get_version() {
    gnome-shell --version | awk '{print $3}'
}

# Find existing extracted versions
get_extracted_versions() {
    if [ -d "$UNTRACKED_DIR" ]; then
        find "$UNTRACKED_DIR" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; 2>/dev/null || true
    fi
}

# Check if source is already extracted for current version
is_extracted() {
    local version="$1"
    local extract_dir="$UNTRACKED_DIR/$version/js-extracted"
    [ -d "$extract_dir" ] && [ -f "$extract_dir/org/gnome/shell/ui/main.js" ]
}

# Remove old extracted versions
cleanup_old_versions() {
    local current_version="$1"
    local old_versions
    old_versions=$(get_extracted_versions)

    for old_version in $old_versions; do
        if [ "$old_version" != "$current_version" ]; then
            echo "Removing old version: $old_version"
            rm -rf "${UNTRACKED_DIR:?}/$old_version"
        fi
    done
}

# Extract JS files from GResource
extract_js() {
    local version="$1"
    local extract_dir="$UNTRACKED_DIR/$version/js-extracted"

    if [ ! -f "$GRESOURCE_FILE" ]; then
        echo "ERROR: GResource file not found: $GRESOURCE_FILE" >&2
        exit 1
    fi

    echo "Extracting GNOME Shell $version JS source..."
    mkdir -p "$extract_dir"

    local count=0
    while IFS= read -r resource; do
        local dest_path="${extract_dir}${resource}"
        mkdir -p "$(dirname "$dest_path")"
        gresource extract "$GRESOURCE_FILE" "$resource" > "$dest_path"
        count=$((count + 1))
    done < <(gresource list "$GRESOURCE_FILE" | grep '\.js$')

    echo "Extracted $count JS files to: $extract_dir"
}

# Main
main() {
    local version
    version=$(get_version)

    echo "GNOME Shell version: $version"

    if is_extracted "$version"; then
        echo "Source already extracted for version $version"
        echo "Location: $UNTRACKED_DIR/$version/js-extracted"
        exit 0
    fi

    # Clean up old versions before extracting new
    cleanup_old_versions "$version"

    # Extract current version
    extract_js "$version"

    echo ""
    echo "Key files for extension development:"
    ls -1 "$UNTRACKED_DIR/$version/js-extracted/org/gnome/shell/ui/" | head -20
}

main "$@"
