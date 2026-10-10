#!/usr/bin/env python3
"""CLI entry point for the closeout checker."""
import sys
sys.dont_write_bytecode = True
from check_closeout import main

if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError, ImportError) as error:
        print(f"closeout-check: FAIL: {error}", file=sys.stderr)
        sys.exit(1)
