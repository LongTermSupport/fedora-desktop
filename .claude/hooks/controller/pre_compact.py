#!/usr/bin/env python3
"""PreCompact front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.pre_compact import (
    WorkflowStatePreCompactHandler,
)


def main():
    """Register all PreCompact handlers and dispatch."""
    controller = FrontController("PreCompact")

    # Register workflow state preservation handler
    controller.register(WorkflowStatePreCompactHandler())

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
