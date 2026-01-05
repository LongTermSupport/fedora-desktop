#!/usr/bin/env python3
"""UserPromptSubmit front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.user_prompt_submit.prompt_handlers import AutoContinueHandler


def main():
    """Register all UserPromptSubmit handlers and dispatch."""
    controller = FrontController("UserPromptSubmit")

    # Register handlers
    controller.register(AutoContinueHandler())

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
