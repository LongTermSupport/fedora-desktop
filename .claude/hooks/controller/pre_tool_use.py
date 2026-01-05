#!/usr/bin/env python3
"""PreToolUse front controller - dispatches to appropriate handler."""

import sys
import os

# Add current directory to Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from front_controller import FrontController
from handlers.pre_tool_use import (
    DestructiveGitHandler,
    GitStashHandler,
    WorktreeFileCopyHandler,
    EnforceControllerPatternHandler,
    EslintDisableHandler,
    PlanTimeEstimatesHandler,
    MarkdownOrganizationHandler,
    WebSearchYearHandler,
    OfficialPlanCommandHandler,
    BritishEnglishHandler,
    TddEnforcementHandler,
    AbsolutePathHandler,
    ValidatePlanNumberHandler,
    PlanWorkflowHandler,
    SedBlockerHandler,
)


def main():
    """Register all PreToolUse handlers and dispatch."""
    controller = FrontController("PreToolUse")

    # Register handler instances in priority order (lower = runs first)
    # Architecture enforcement (priority 5)
    controller.register(EnforceControllerPatternHandler())  # priority=5

    # Safety checks first (priority 10-20)
    controller.register(DestructiveGitHandler())         # priority=10
    controller.register(SedBlockerHandler())             # priority=10 (sed safety)
    controller.register(AbsolutePathHandler())           # priority=12 (path validation)
    controller.register(TddEnforcementHandler())         # priority=15 (TDD enforcement)
    controller.register(WorktreeFileCopyHandler())       # priority=15
    controller.register(GitStashHandler())               # priority=20

    # Workflow enforcement (priority 25-60)
    controller.register(OfficialPlanCommandHandler())      # priority=25
    controller.register(EslintDisableHandler())            # priority=30
    controller.register(ValidatePlanNumberHandler())       # priority=30 (plan number validation)
    controller.register(MarkdownOrganizationHandler())   # priority=35
    controller.register(PlanTimeEstimatesHandler())      # priority=40
    controller.register(PlanWorkflowHandler())           # priority=45 (plan guidance)
    controller.register(WebSearchYearHandler())          # priority=55
    controller.register(BritishEnglishHandler())         # priority=60

    # Run dispatcher
    controller.run()


if __name__ == '__main__':
    main()
