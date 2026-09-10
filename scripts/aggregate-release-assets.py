#!/usr/bin/env python3
"""Validate and combine the exact two same-run native build artifacts."""

from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import stat
from pathlib import Path


TAG_RE = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+\Z")


def fail(message: str) -> None:
    raise SystemExit(message)


def exact_directory(path: Path, expected: set[str]) -> dict[str, Path]:
    entries = list(os.scandir(path))
    if len(entries) != len(expected) or {entry.name for entry in entries} != expected:
        fail(f"unexpected same-run artifact set: {path.name}")
    result: dict[str, Path] = {}
    for entry in entries:
        mode = entry.stat(follow_symlinks=False).st_mode
        if not stat.S_ISREG(mode) or entry.is_symlink():
            fail(f"same-run artifacts must be regular files: {path.name}/{entry.name}")
        result[entry.name] = Path(entry.path)
    return result


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", required=True)
    parser.add_argument("--incoming", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if not TAG_RE.fullmatch(args.tag):
        fail("experimental tag required")
    if not args.incoming.is_dir():
        fail("incoming artifact directory missing")
    top = list(os.scandir(args.incoming))
    if len(top) != 2 or {entry.name for entry in top} != {"linux-amd64", "darwin-arm64"}:
        fail("unexpected same-run artifact directories")
    for entry in top:
        mode = entry.stat(follow_symlinks=False).st_mode
        if not stat.S_ISDIR(mode) or entry.is_symlink():
            fail("same-run artifact entries must be real directories")
    source_name = f"agent-governance-evidence_{args.tag}_source.tar.gz"
    linux_name = f"agent-governance-evidence_{args.tag}_linux-amd64.tar.gz"
    darwin_name = f"agent-governance-evidence_{args.tag}_darwin-arm64.tar.gz"
    linux = exact_directory(args.incoming / "linux-amd64", {linux_name, source_name})
    darwin = exact_directory(args.incoming / "darwin-arm64", {darwin_name, source_name})
    if linux[source_name].read_bytes() != darwin[source_name].read_bytes():
        fail("native builds produced different source archives")
    parent = args.output.parent.resolve(strict=True)
    output = parent / args.output.name
    try:
        output.mkdir(mode=0o700)
    except FileExistsError:
        fail("refusing existing aggregate output")
    complete = False
    try:
        selected = (linux[linux_name], darwin[darwin_name], linux[source_name])
        for source in selected:
            shutil.copyfile(source, output / source.name)
            os.chmod(output / source.name, 0o644)
        with (output / "SHA256SUMS").open("x", encoding="ascii", newline="\n") as sums:
            for source in selected:
                target = output / source.name
                sums.write(f"{hashlib.sha256(target.read_bytes()).hexdigest()}  {target.name}\n")
        os.chmod(output / "SHA256SUMS", 0o644)
        os.chmod(output, 0o755)
        complete = True
    finally:
        if not complete and output.exists() and not output.is_symlink():
            shutil.rmtree(output)


if __name__ == "__main__":
    main()
