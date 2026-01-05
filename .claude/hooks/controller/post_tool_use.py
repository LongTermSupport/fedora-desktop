#!/usr/bin/env python3
"""PostToolUse front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.post_tool_use.file_handlers import (
    ValidateEslintOnWriteHandler,
    ValidateSitemapHandler,
)


def main():
    """Register all PostToolUse handlers and dispatch."""
    controller = FrontController("PostToolUse")

    # Register all PostToolUse handler instances in priority order
    controller.register(ValidateEslintOnWriteHandler())    # priority=10
    controller.register(ValidateSitemapHandler())          # priority=20
    # ValidatePlanNumberHandler moved to PreToolUse to fix timing bug

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
