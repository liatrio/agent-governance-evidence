#!/usr/bin/env python3
"""Reject skipped Go tests and unreadable test logs."""

from __future__ import annotations

import re
import sys
from pathlib import Path


if len(sys.argv) != 2:
    raise SystemExit("usage: check-go-test-log.py LOG")
try:
    body = Path(sys.argv[1]).read_text(encoding="utf-8")
except (OSError, UnicodeError) as error:
    raise SystemExit(f"unable to inspect Go test skips: {error}")
if re.search(r"--- SKIP:|^\s*SKIP\s*$", body, re.MULTILINE):
    raise SystemExit("skipped Go tests are not accepted")
