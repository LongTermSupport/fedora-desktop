#!/usr/bin/bash
# Run all QA checks for the entire project

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FAILED=0

echo "╔════════════════════════════════════════════════════════════╗"
echo "║  Repository QA - All Scripts                               ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""

# Python QA
if "$SCRIPT_DIR/qa-python.bash"; then
    echo ""
else
    echo ""
    echo "⚠ Python QA failed"
    ((FAILED++))
fi

# Bash QA
if "$SCRIPT_DIR/qa-bash.bash"; then
    echo ""
else
    echo ""
    echo "⚠ Bash QA failed"
    ((FAILED++))
fi

echo "╔════════════════════════════════════════════════════════════╗"
if [ $FAILED -eq 0 ]; then
    echo "║  ✓ ALL QA CHECKS PASSED                                    ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    exit 0
else
    echo "║  ✗ $FAILED QA CHECK(S) FAILED                                   ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    exit 1
fi
