#!/usr/bin/env bash
set -euo pipefail

# Resolve the release/package version used by GitHub Actions and local release
# scripts. Brandon's standard release version style is date-based, matching the
# ZFS plugin pattern: YYYY.MM.DD.tNN, where NN increments per release attempt on
# the same Central-time date.
#
# Precedence:
#   1. Explicit version argument, if supplied.
#   2. Git tag name, with a leading v stripped, when running from a tag.
#   3. Next Central-time date version, derived from existing vYYYY.MM.DD.tNN tags.

explicit="${1:-}"
if [[ -n "$explicit" ]]; then
  version="$explicit"
elif [[ "${GITHUB_REF_TYPE:-}" == "tag" && -n "${GITHUB_REF_NAME:-}" ]]; then
  version="${GITHUB_REF_NAME#v}"
else
  release_date="${FSE_RELEASE_DATE:-$(TZ=America/Chicago date +%Y.%m.%d)}"
  if ! [[ "$release_date" =~ ^[0-9]{4}\.[0-9]{2}\.[0-9]{2}$ ]]; then
    printf 'invalid FSE_RELEASE_DATE %q; expected YYYY.MM.DD\n' "$release_date" >&2
    exit 1
  fi

  # Fetch tags opportunistically so workflow_dispatch runs can increment from
  # the repository's existing published date tags. Keep local/no-network use
  # working by ignoring fetch failures.
  git fetch --tags --force --quiet 2>/dev/null || true

  last_suffix="$({
    git tag --list "v${release_date}.t[0-9][0-9]" 2>/dev/null || true
    git tag --list "${release_date}.t[0-9][0-9]" 2>/dev/null || true
  } | sed -E "s/^v?${release_date}\.t//" | sort -n | tail -n 1)"
  if [[ -z "$last_suffix" ]]; then
    next=1
  else
    next=$((10#$last_suffix + 1))
  fi
  printf -v version '%s.t%02d' "$release_date" "$next"
fi

if ! [[ "$version" =~ ^[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.t[0-9]{2}$ ]]; then
  printf 'invalid release version %q; expected YYYY.MM.DD.tNN\n' "$version" >&2
  exit 1
fi

printf '%s\n' "$version"
