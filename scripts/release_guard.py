#!/usr/bin/env python3
"""Strict local integrity checks for a downloaded evidence prerelease."""

from __future__ import annotations

import argparse
import hashlib
import io
import os
import re
import stat
import subprocess
import tarfile
from pathlib import Path


TAG_RE = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+\Z")
SHA_RE = re.compile(r"[0-9a-f]{40}\Z")
HASH_LINE_RE = re.compile(r"([0-9a-f]{64})  ([^/\s]+)\n\Z")


def fail(message: str) -> None:
    raise SystemExit(message)


def release_names(tag: str) -> tuple[str, str, str, str, str]:
    return (
        f"agent-governance-evidence_{tag}_linux-amd64.tar.gz",
        f"agent-governance-evidence_{tag}_darwin-arm64.tar.gz",
        f"agent-governance-evidence_{tag}_source.tar.gz",
        "SHA256SUMS",
        "build-provenance.sigstore.json",
    )


def require_regular_assets(assets: Path, names: tuple[str, ...]) -> None:
    entries = list(os.scandir(assets))
    actual = {entry.name for entry in entries}
    if actual != set(names) or len(entries) != len(names):
        fail("release asset set is not exact")
    for entry in entries:
        mode = entry.stat(follow_symlinks=False).st_mode
        if not stat.S_ISREG(mode) or entry.is_symlink():
            fail("release assets must be regular files")


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def require_checksums(assets: Path, archives: tuple[str, str, str]) -> None:
    try:
        lines = (assets / "SHA256SUMS").read_text(encoding="ascii").splitlines(keepends=True)
    except UnicodeDecodeError:
        fail("malformed checksum manifest")
    parsed: list[tuple[str, str]] = []
    for line in lines:
        match = HASH_LINE_RE.fullmatch(line)
        if not match:
            fail("malformed checksum manifest")
        parsed.append((match.group(1), match.group(2)))
    if len(parsed) != 3 or {name for _, name in parsed} != set(archives):
        fail("checksum manifest names are not exact")
    if len({name for _, name in parsed}) != 3:
        fail("checksum manifest contains duplicate names")
    for expected, name in parsed:
        if digest(assets / name) != expected:
            fail(f"checksum mismatch: {name}")


def archive_members(path: Path) -> list[tarfile.TarInfo]:
    try:
        with tarfile.open(path, "r:gz") as archive:
            return archive.getmembers()
    except (tarfile.TarError, OSError) as error:
        fail(f"invalid archive {path.name}: {error}")


def require_platform_archive(path: Path, license_bytes: bytes) -> None:
    members = archive_members(path)
    expected = {
        "agent-governance-evidence": 0o755,
        "agent-governance-demo": 0o755,
        "checkpoint": 0o755,
        "LICENSE": 0o644,
    }
    if len(members) != len(expected) or {member.name for member in members} != set(expected):
        fail(f"bad archive membership: {path.name}")
    for member in members:
        if not member.isreg() or member.name.startswith("/") or "/" in member.name:
            fail(f"unsafe binary archive member: {path.name}")
        if member.mode != expected[member.name]:
            fail(f"bad archive modes: {path.name}")
        with tarfile.open(path, "r:gz") as archive:
            extracted = archive.extractfile(member)
            if extracted is None:
                fail(f"unable to read archive member: {path.name}")
            if member.name == "LICENSE" and extracted.read() != license_bytes:
                fail(f"platform LICENSE differs from trusted source: {path.name}")


def require_safe_name(name: str, prefix: str) -> None:
    root = prefix.rstrip("/")
    if name == root:
        return
    if not name.startswith(prefix) or name.startswith("/"):
        fail("unsafe source archive member or type")
    suffix = name[len(prefix) :]
    components = suffix.rstrip("/").split("/")
    if not suffix.rstrip("/") or any(component in {"", ".", ".."} for component in components):
        fail("unsafe source archive member or type")


def archive_index(path: Path, prefix: str) -> dict[str, tuple[str, int, bytes]]:
    members = archive_members(path)
    output: dict[str, tuple[str, int, bytes]] = {}
    try:
        with tarfile.open(path, "r:*") as archive:
            for member in members:
                require_safe_name(member.name, prefix)
                if not (member.isreg() or member.isdir()) or member.issym() or member.islnk() or member.isdev() or member.isfifo():
                    fail("unsafe source archive member or type")
                if member.name in output:
                    fail("duplicate source archive member")
                if member.isdir():
                    output[member.name] = ("directory", member.mode, b"")
                    continue
                extracted = archive.extractfile(member)
                if extracted is None:
                    fail("unable to read source archive member")
                output[member.name] = ("file", member.mode, extracted.read())
    except (tarfile.TarError, OSError) as error:
        fail(f"invalid source archive: {error}")
    return output


def trusted_source_index(source: Path, commit: str, prefix: str) -> dict[str, tuple[str, int, bytes]]:
    if not (source / ".git").exists():
        fail("trusted source checkout is not a git repository")
    if subprocess.run(["git", "-C", source, "diff", "--quiet"], check=False).returncode:
        fail("trusted source checkout is dirty")
    if subprocess.run(["git", "-C", source, "diff", "--cached", "--quiet"], check=False).returncode:
        fail("trusted source checkout is dirty")
    if subprocess.check_output(["git", "-C", source, "status", "--porcelain", "--untracked-files=all"], text=True):
        fail("trusted source checkout is dirty")
    head = subprocess.check_output(["git", "-C", source, "rev-parse", "HEAD"], text=True).strip()
    if head != commit:
        fail("trusted source checkout commit mismatch")
    raw = subprocess.check_output(
        ["git", "-C", source, "-c", "tar.umask=022", "archive", "--format=tar", f"--prefix={prefix}", commit]
    )
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
        expected: dict[str, tuple[str, int, bytes]] = {}
        for member in archive.getmembers():
            require_safe_name(member.name, prefix)
            if not (member.isreg() or member.isdir()) or member.name in expected:
                fail("trusted source archive is unsafe")
            if member.isdir():
                expected[member.name] = ("directory", member.mode, b"")
                continue
            extracted = archive.extractfile(member)
            if extracted is None:
                fail("unable to read trusted source archive")
            expected[member.name] = ("file", member.mode, extracted.read())
        return expected


def require_source_binding(assets: Path, source: Path, tag: str, commit: str) -> dict[str, tuple[str, int, bytes]]:
    prefix = f"agent-governance-evidence_{tag}/"
    actual = archive_index(assets / release_names(tag)[2], prefix)
    expected = trusted_source_index(source, commit, prefix)
    if actual != expected:
        fail("source archive does not exactly match trusted git archive")
    return expected


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--assets-dir", required=True, type=Path)
    parser.add_argument("--source-dir", required=True, type=Path)
    args = parser.parse_args()
    if not TAG_RE.fullmatch(args.tag) or not SHA_RE.fullmatch(args.commit):
        fail("invalid tag or commit")
    if not args.assets_dir.is_dir():
        fail("assets directory missing")
    names = release_names(args.tag)
    require_regular_assets(args.assets_dir, names)
    require_checksums(args.assets_dir, names[:3])
    expected_source = require_source_binding(args.assets_dir, args.source_dir, args.tag, args.commit)
    license_entry = expected_source.get(f"agent-governance-evidence_{args.tag}/LICENSE")
    if license_entry is None or license_entry[0] != "file":
        fail("trusted source LICENSE missing")
    require_platform_archive(args.assets_dir / names[0], license_entry[2])
    require_platform_archive(args.assets_dir / names[1], license_entry[2])


if __name__ == "__main__":
    main()
