#!/usr/bin/env python3
"""SubagentStop front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.subagent_stop.agent_handlers import RemindPromptLibraryHandler, RemindValidatorHandler


def main():
    """Register all SubagentStop handlers and dispatch."""
    controller = FrontController("SubagentStop")

    # Register handlers (priority order)
    controller.register(RemindValidatorHandler())        # priority=50 (specific validators)
    controller.register(RemindPromptLibraryHandler())    # priority=100 (general reminder)

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
