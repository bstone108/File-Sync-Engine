#!/usr/bin/env bash
set -euo pipefail

# Resolve the published File Sync Engine release version used by the Release
# artifacts workflow, git tags, GitHub Releases, and GHCR images.
#
# Date.build versions apply ONLY to those published-release surfaces. They are
# never used for go test, the serious harness, PR CI (.github/workflows/ci.yml),
# or local smoke/dev builds. Those paths keep dummy/dev versions such as
# 0.1.0-dev, 0.1.99-test, and ci-<sha>.
#
# Format is YYYY.M.D.N with no zero-padding, using the America/Chicago calendar
# day. Example: first published build on 24 August 2026 is 2026.8.24.1; the
# second that same Chicago day is 2026.8.24.2. Git tags keep the v prefix
# (v2026.8.24.1); this script prints the version without v.
#
# Historical published tags used zero-padded YYYY.MM.DD.NN (v2026.08.11.01).
# New versions are always emitted unpadded. When choosing the next N, both
# padded and unpadded tags/releases for that Chicago calendar day count, so
# a new unpadded stamp cannot collide with an old padded one.
#
# Precedence:
#   1. Explicit version argument, if supplied and non-empty.
#   2. Git tag name, with a leading v stripped, when GITHUB_REF_TYPE=tag.
#   3. Otherwise auto-generate: America/Chicago today's YYYY.M.D, then
#      N = 1 + max existing N for that day from local tags, remote tags, and
#      GitHub releases. If none exist, N=1.
#
# FSE_RELEASE_DATE (YYYY-MM-DD) is a test-only override for the Chicago
# calendar day. It is rejected when GITHUB_ACTIONS=true.

usage_error() {
  printf '%s\n' "$1" >&2
  exit 1
}

strip_tag_ref() {
  local ref="$1"
  ref="${ref#refs/tags/}"
  if [[ "$ref" == *'^{}' ]]; then
    ref="${ref%'^{}'}"
  fi
  printf '%s\n' "${ref#v}"
}

# Print "year month day n" for a padded or unpadded YYYY.M.D.N / YYYY.MM.DD.NN
# value. Optional leading v / refs/tags/ / ^{} is ignored. Returns 1 if the
# string is not a date.build version.
parse_release_version() {
  local raw
  raw="$(strip_tag_ref "$1")"
  if [[ ! "$raw" =~ ^([0-9]{4})\.([0-9]{1,2})\.([0-9]{1,2})\.([0-9]{1,})$ ]]; then
    return 1
  fi
  local year month day n
  year="${BASH_REMATCH[1]}"
  month=$((10#${BASH_REMATCH[2]}))
  day=$((10#${BASH_REMATCH[3]}))
  n=$((10#${BASH_REMATCH[4]}))
  if (( year < 1 || month < 1 || month > 12 || day < 1 || day > 31 || n < 1 )); then
    return 1
  fi
  printf '%s %s %s %s\n' "$year" "$month" "$day" "$n"
}

canonicalize_release_version() {
  local parsed year month day n
  parsed="$(parse_release_version "$1")" || return 1
  read -r year month day n <<<"$parsed"
  printf '%s.%s.%s.%s\n' "$year" "$month" "$day" "$n"
}

chicago_calendar_day() {
  local ymd year month day
  if [[ -n "${FSE_RELEASE_DATE:-}" ]]; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'FSE_RELEASE_DATE is test-only and must not be set during GitHub Actions releases'
    fi
    ymd="$FSE_RELEASE_DATE"
  else
    # GitHub-hosted runners are UTC. The published date.build calendar day is
    # America/Chicago, never UTC.
    ymd="$(TZ=America/Chicago date +%Y-%m-%d)"
  fi
  if [[ ! "$ymd" =~ ^([0-9]{4})-([0-9]{2})-([0-9]{2})$ ]]; then
    usage_error "invalid Chicago calendar day ${ymd@Q}; expected YYYY-MM-DD"
  fi
  year="${BASH_REMATCH[1]}"
  month=$((10#${BASH_REMATCH[2]}))
  day=$((10#${BASH_REMATCH[3]}))
  printf '%s %s %s\n' "$year" "$month" "$day"
}

consider_ref() {
  local parsed year month day n
  parsed="$(parse_release_version "$1")" || return 0
  read -r year month day n <<<"$parsed"
  if (( year == want_year && month == want_month && day == want_day && n > max_n )); then
    max_n=$n
  fi
}

list_local_tags() {
  git tag --list 'v*' 2>/dev/null || true
}

list_remote_tags() {
  if ! git remote get-url origin >/dev/null 2>&1; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'origin remote is required to auto-generate a published release version'
    fi
    return 0
  fi
  local listing
  if ! listing="$(git ls-remote --tags origin 2>/dev/null)"; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'failed to list remote tags; refusing to invent a published release version'
    fi
    return 0
  fi
  printf '%s\n' "$listing"
}

list_github_release_tags() {
  if ! command -v gh >/dev/null 2>&1; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'gh is required to auto-generate a published release version in GitHub Actions'
    fi
    return 0
  fi
  local token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
  if [[ -z "$token" ]]; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'GH_TOKEN is required to auto-generate a published release version in GitHub Actions'
    fi
    return 0
  fi
  local repo_args=()
  if [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
    repo_args=(--repo "$GITHUB_REPOSITORY")
  fi
  local listing
  if ! listing="$(GH_TOKEN="$token" gh release list --limit 1000 --json tagName --jq '.[].tagName' "${repo_args[@]}")"; then
    if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
      usage_error 'failed to list GitHub releases; refusing to invent a published release version'
    fi
    return 0
  fi
  printf '%s\n' "$listing"
}

auto_generate_version() {
  local want_year want_month want_day max_n=0
  read -r want_year want_month want_day <<<"$(chicago_calendar_day)"

  local tag
  while IFS= read -r tag; do
    [[ -n "$tag" ]] || continue
    consider_ref "$tag"
  done < <(list_local_tags)

  local line ref
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    ref="${line##*$'\t'}"
    [[ -n "$ref" ]] || continue
    consider_ref "$ref"
  done < <(list_remote_tags)

  while IFS= read -r tag; do
    [[ -n "$tag" ]] || continue
    consider_ref "$tag"
  done < <(list_github_release_tags)

  printf '%s.%s.%s.%s\n' "$want_year" "$want_month" "$want_day" "$((max_n + 1))"
}

if [[ -n "${FSE_RELEASE_DATE:-}" && "${GITHUB_ACTIONS:-}" == "true" ]]; then
  usage_error 'FSE_RELEASE_DATE is test-only and must not be set during GitHub Actions releases'
fi

explicit="${1:-}"
if [[ -n "$explicit" ]]; then
  version="$explicit"
elif [[ "${GITHUB_REF_TYPE:-}" == "tag" && -n "${GITHUB_REF_NAME:-}" ]]; then
  version="${GITHUB_REF_NAME#v}"
else
  canonicalize_release_version "$(auto_generate_version)"
  exit 0
fi

raw_version="$version"
if ! version="$(canonicalize_release_version "$raw_version")"; then
  usage_error "invalid release version ${raw_version}; expected YYYY.M.D.N (unpadded; leftover YYYY.MM.DD.NN is accepted and canonicalized)"
fi

printf '%s\n' "$version"
