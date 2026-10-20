#!/usr/bin/env python3
"""Reject duplicate mapping keys in the CI YAML.

`yaml.safe_load` ACCEPTS duplicate keys (last one wins), so a duplicated env var
in compose.yaml parses fine locally and only explodes on the runner with
"mapping key ... already defined". Checking it here keeps that failure local.
"""

import sys

import yaml


class Strict(yaml.SafeLoader):
    """SafeLoader that refuses duplicate mapping keys."""


def no_duplicates(loader, node, deep=False):
    seen = set()
    for key_node, _ in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in seen:
            raise ValueError(f"duplicate key {key!r} at line {key_node.start_mark.line + 1}")
        seen.add(key)
    return yaml.SafeLoader.construct_mapping(loader, node, deep)


Strict.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, no_duplicates)


def main(paths):
    failed = False
    for path in paths:
        try:
            with open(path, encoding="utf-8") as fh:
                yaml.load(fh, Loader=Strict)
            print(f"OK   {path}")
        except Exception as exc:  # noqa: BLE001 - report every file
            print(f"FAIL {path}: {exc}")
            failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:] or [".github/ci/compose.yaml", ".github/workflows/ci.yml"]))
