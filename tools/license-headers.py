#!/usr/bin/env python3
"""Apply the Apache 2.0 SPDX header to every source file KNOTT owns.

Two lines rather than the full boilerplate: SPDX identifiers are machine
readable, tooling understands them, and a fourteen-line comment at the top of
every file is noise for the humans who actually read these.

Usage:
    python tools/license-headers.py          # apply, reporting what changed
    python tools/license-headers.py --check  # exit non-zero if any are missing
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
HOLDER = "Regnant"
YEAR = "2026"
SPDX = "SPDX-License-Identifier: Apache-2.0"
COPYRIGHT = f"Copyright {YEAR} {HOLDER}"

# Extension -> (line prefix, files to skip because they are generated).
COMMENT = {
    ".go": "//",
    ".js": "//",
    ".jsx": "//",
    ".py": "#",
    ".sh": "#",
}

SKIP_DIRS = {"node_modules", "dist", "bin", ".git", "data", "logs", ".scratch"}
SKIP_FILES = {
    # Generated from tools/brand/generate.py; the header would be regenerated away.
    "apps/designer/src/components/Brand.jsx",
}


def tracked_files() -> list[Path]:
    out = subprocess.run(["git", "ls-files"], cwd=ROOT, capture_output=True, text=True)
    files = []
    for line in out.stdout.splitlines():
        p = Path(line)
        if any(part in SKIP_DIRS for part in p.parts):
            continue
        if line in SKIP_FILES:
            continue
        if p.suffix in COMMENT:
            files.append(ROOT / p)
    return files


def header_for(prefix: str) -> str:
    return f"{prefix} {COPYRIGHT}\n{prefix} {SPDX}\n"


def apply(path: Path, check: bool) -> bool:
    """Returns True when the file was (or would be) changed."""
    prefix = COMMENT[path.suffix]
    text = path.read_text(encoding="utf-8")
    if SPDX in text[:800]:
        return False
    if check:
        return True

    lines = text.splitlines(keepends=True)
    insert_at = 0
    # A shebang and any build constraint must stay on the first lines.
    if lines and lines[0].startswith("#!"):
        insert_at = 1
    while insert_at < len(lines) and lines[insert_at].startswith("//go:build"):
        insert_at += 1
    # Keep a blank line between what came before and the header.
    block = header_for(prefix)
    if insert_at > 0 and not lines[insert_at - 1].isspace():
        block = "\n" + block
    if insert_at < len(lines) and lines[insert_at].strip():
        block += "\n"

    lines.insert(insert_at, block)
    path.write_text("".join(lines), encoding="utf-8", newline="\n")
    return True


def main() -> int:
    check = "--check" in sys.argv
    changed = [p for p in tracked_files() if apply(p, check)]
    if not changed:
        print("All source files carry the licence header.")
        return 0
    verb = "missing the header" if check else "updated"
    print(f"{len(changed)} file(s) {verb}:")
    for p in changed:
        print(f"  {p.relative_to(ROOT).as_posix()}")
    return 1 if check else 0


if __name__ == "__main__":
    raise SystemExit(main())
