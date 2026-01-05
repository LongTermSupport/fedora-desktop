#!/usr/bin/env python3
"""SessionStart front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.session_start import (
    WorkflowStateRestorationHandler,
)


def main():
    """Register all SessionStart handlers and dispatch."""
    controller = FrontController("SessionStart")

    # Register workflow state restoration handler
    controller.register(WorkflowStateRestorationHandler())

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
