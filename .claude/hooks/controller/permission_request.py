#!/usr/bin/env python3
"""PermissionRequest front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
# from handlers.permission_request import (
#     # Register handlers here as they're implemented
# )


def main():
    """Register all PermissionRequest handlers and dispatch."""
    controller = FrontController("PermissionRequest")

    # Register handler instances here (none implemented yet)
    # Example: controller.register(MyHandler())

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
