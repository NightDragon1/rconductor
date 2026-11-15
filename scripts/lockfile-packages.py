#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Lists what a change does to the lockfiles (PRJ-005, SEC-013), for the
# pull request text: packages that are new with name@version and origin,
# and entries whose origin or checksum changed without a version change.
# Usage: scripts/lockfile-packages.py <base-ref> [<head-ref>]
import json
import os
import subprocess
import sys

NPM = ["web/package-lock.json"]
GOSUM = ["go.sum", "tools/go.sum"]


def show(ref, path):
    """The file at ref; a file missing there counts as empty."""
    r = subprocess.run(
        ["git", "show", f"{ref}:{path}"], capture_output=True, text=True
    )
    return r.stdout if r.returncode == 0 else ""


def check_ref(ref):
    r = subprocess.run(
        ["git", "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"],
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        sys.exit(f"unknown ref: {ref}")


def npm(ref, path):
    """(key, name, version) -> (resolved, integrity)."""
    text = show(ref, path)
    if not text.strip():
        return {}
    out = {}
    for key, p in json.loads(text).get("packages", {}).items():
        if "node_modules/" not in key:
            continue  # the root project and workspace folders
        name = key.rsplit("node_modules/", 1)[1]
        out[(key, name, p.get("version", "?"))] = (
            p.get("resolved", "?"),
            p.get("integrity", "?"),
        )
    return out


def gosum(ref, path):
    """(module, version) -> h1 hash; /go.mod lines are left out."""
    out = {}
    for line in show(ref, path).splitlines():
        parts = line.split()
        if len(parts) == 3 and not parts[1].endswith("/go.mod"):
            out[(parts[0], parts[1])] = parts[2]
    return out


def section(title, lines):
    print(f"{title}:")
    lines = sorted(set(lines))
    for line in lines:
        print(f"- {line}")
    if not lines:
        print("none")


def report_npm(path, base, head):
    old, new = npm(base, path), npm(head, path)
    old_pairs = {(name, ver) for _, name, ver in old}
    added = [
        f"{name}@{ver} ({new[(key, name, ver)][0]})"
        for key, name, ver in new
        if (name, ver) not in old_pairs
    ]
    changed = []
    for k in sorted(set(old) & set(new)):
        key, name, ver = k
        for i, field in enumerate(["resolved", "integrity"]):
            if old[k][i] != new[k][i]:
                changed.append(
                    f"{name}@{ver} ({key}) {field}: {old[k][i]} -> {new[k][i]}"
                )
    print(f"## {path}")
    section("New packages", added)
    section("Changed origin or checksum", changed)


def report_gosum(path, base, head):
    old, new = gosum(base, path), gosum(head, path)
    added = [
        f"{mod}@{ver} (proxy.golang.org, {mod})"
        for mod, ver in new
        if (mod, ver) not in old
    ]
    changed = [
        f"{mod}@{ver} hash: {old[(mod, ver)]} -> {new[(mod, ver)]}"
        for mod, ver in set(old) & set(new)
        if old[(mod, ver)] != new[(mod, ver)]
    ]
    print(f"## {path}")
    section("New packages", added)
    section("Changed origin or checksum", changed)


def main():
    if len(sys.argv) not in (2, 3):
        sys.exit("usage: scripts/lockfile-packages.py <base-ref> [<head-ref>]")
    base = sys.argv[1]
    head = sys.argv[2] if len(sys.argv) == 3 else "HEAD"
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    check_ref(base)
    check_ref(head)
    first = True
    for path in NPM + GOSUM:
        if not first:
            print()
        first = False
        if path in NPM:
            report_npm(path, base, head)
        else:
            report_gosum(path, base, head)


if __name__ == "__main__":
    main()
