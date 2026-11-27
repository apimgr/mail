#!/usr/bin/env bash
# shellcheck shell=bash
# - - - - - - - - - - - - - - - - - - - - - - - - -
##@Version           :  YYYYMMDDHHMM-git
# @@Author           :  Jason Hempstead
# @@Contact          :  git-admin@casjaysdev.pro
# @@License          :  WTFPL
# @@ReadME            :  README.md
# @@Copyright        :  Copyright: (c) 2026 Jason Hempstead, Casjays Developments
# @@Created          :  Thursday, November 26, 2026 03:52 EST
# @@File             :  verify-licenses.sh
# @@Description      :  Scan shipped Go packages for incompatible licenses.
# @@Changelog        :  Add the required dependency license verification and inventory.
# @@TODO             :  None
# @@Other            :  Run inside casjaysdev/go:latest.
# @@Resource         :  AI.md PART 2
# @@Terminal App     :  no
# @@sudo/root        :  no
# @@Template         :  shell/bash
# - - - - - - - - - - - - - - - - - - - - - - - - -
# shellcheck disable=SC1001,SC1003,SC2001,SC2003,SC2016,SC2031,SC2034,SC2090,SC2115,SC2120,SC2155,SC2199,SC2229,SC2317,SC2329
# - - - - - - - - - - - - - - - - - - - - - - - - -
APPNAME="${0##*/}"
VERSION="YYYYMMDDHHMM-git"
RUN_USER="${USER:-unknown}"
SET_UID="${UID:-$(id -u)}"
SCRIPT_SRC_DIR="${BASH_SOURCE%/*}"
PROJECT_ROOT="$(cd "${SCRIPT_SRC_DIR}/.." && pwd)"
PROJECT_ORG="$(awk -F: '/^[[:space:]]*project_org:/ {sub(/^[[:space:]]+/, "", $2); sub(/[[:space:]#].*$/, "", $2); print $2; exit}' "${PROJECT_ROOT}/IDEA.md")"
INTERNAL_NAME="$(awk -F: '/^[[:space:]]*internal_name:/ {sub(/^[[:space:]]+/, "", $2); sub(/[[:space:]#].*$/, "", $2); print $2; exit}' "${PROJECT_ROOT}/IDEA.md")"
if [[ -z "${PROJECT_ORG}" ]]; then
  PROJECT_ORG="$(git -C "${PROJECT_ROOT}" remote get-url origin 2>/dev/null | sed -E 's#.*[:/]([^/]+)/[^/]+(\.git)?$#\1#' || true)"
fi
EXIT_STATUS=0

# - - - - - - - - - - - - - - - - - - - - - - - - -
set -eo pipefail

__fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit "$2"
}

# - - - - - - - - - - - - - - - - - - - - - - - - -
if [[ -z "${PROJECT_ORG}" || -z "${INTERNAL_NAME}" ]]; then
  __fail 'could not determine project organization and internal name' 65
fi

if ! command -v go-licenses >/dev/null 2>&1; then
  __fail 'go-licenses not found; run inside casjaysdev/go:latest' 69
fi

cd "${PROJECT_ROOT}"
mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
LICENSE_TMP="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${INTERNAL_NAME}-XXXXXX")"
trap 'rm -rf -- "${LICENSE_TMP}"' EXIT

IGNORE=()
if [[ -f .github/license-overrides.txt ]]; then
  while read -r module _ _; do
    [[ -z "${module}" || "${module}" == \#* ]] && continue
    IGNORE+=(--ignore "${module}")
  done < .github/license-overrides.txt
fi

go-licenses csv ./src/... "${IGNORE[@]}" > "${LICENSE_TMP}/licenses.csv"
if [[ -f .github/license-overrides.txt ]]; then
  awk 'NF >= 2 && $1 !~ /^#/ {print $1 "," $2}' .github/license-overrides.txt >> "${LICENSE_TMP}/licenses.csv"
fi
if awk -F, 'tolower($2) ~ /(gpl|agpl|lgpl)/ {print; found=1} END {exit !found}' "${LICENSE_TMP}/licenses.csv"; then
  __fail 'copyleft dependency license detected' 1
fi
go-licenses save ./src/... "${IGNORE[@]}" --save_path="${LICENSE_TMP}/third_party_licenses"
printf 'Dependency license report:\n'
cat "${LICENSE_TMP}/licenses.csv"

exit "${EXIT_STATUS}"
# - - - - - - - - - - - - - - - - - - - - - - - - -
# ex: ts=2 sw=2 et filetype=sh
