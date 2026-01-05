#!/usr/bin/env python3
"""Test runner for Claude Code hooks."""

import unittest
import sys
import os

# Add current directory to path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))


def run_tests():
    """Discover and run all tests."""
    # Discover tests
    loader = unittest.TestLoader()
    start_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'tests')
    suite = loader.discover(start_dir, pattern='test_*.py')

    # Run tests
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)

    # Return exit code
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    sys.exit(run_tests())
