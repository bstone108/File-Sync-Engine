#!/usr/bin/env bash
set -euo pipefail

# Resolve the release/package version used by GitHub Actions and local release
# scripts. File Sync Engine main-branch release versions are date-based:
# YYYY.MM.DD.NN. Brandon uses a testing-only letter prefix in other projects'
# testing branches; this repository does not have a testing branch yet.
#
# Important policy: this script does not auto-increment build numbers. A release
# build must be tied to an explicit manual release version or to an already-created
# Git tag. Humans/workers decide when enough changes justify the next NN.
#
# Precedence:
#   1. Explicit version argument, if supplied by workflow_dispatch/local tooling.
#   2. Git tag name, with a leading v stripped, when running from a tag push.
#
# If neither source exists, fail instead of inventing a new version.

explicit="${1:-}"
if [[ -n "$explicit" ]]; then
  version="$explicit"
elif [[ "${GITHUB_REF_TYPE:-}" == "tag" && -n "${GITHUB_REF_NAME:-}" ]]; then
  version="${GITHUB_REF_NAME#v}"
else
  printf 'manual release version required; pass YYYY.MM.DD.NN or run from tag vYYYY.MM.DD.NN\n' >&2
  exit 1
fi

if ! [[ "$version" =~ ^[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]{2}$ ]]; then
  printf 'invalid release version %q; expected YYYY.MM.DD.NN\n' "$version" >&2
  exit 1
fi

printf '%s\n' "$version"
