#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 godevccu authors.
#
# Copies the pydevccu device_descriptions/ and paramset_descriptions/
# JSON files into the location used by the //go:embed directive.
#
# Run before `go build` whenever the upstream pydevccu repository updates.
#
# Usage: ./script/copy_data.sh [path-to-pydevccu]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${1:-${REPO_ROOT}/../pydevccu/pydevccu}"
DST="${REPO_ROOT}/internal/embed/data"

if [[ ! -d "${SRC}/device_descriptions" ]]; then
  echo "error: ${SRC}/device_descriptions not found" >&2
  echo "usage: $0 [path-to-pydevccu]" >&2
  exit 1
fi

mkdir -p "${DST}/device_descriptions" "${DST}/paramset_descriptions"

# Wipe stale files so removed devices do not linger.
find "${DST}/device_descriptions" -name '*.json' -delete
find "${DST}/paramset_descriptions" -name '*.json' -delete

cp "${SRC}/device_descriptions/"*.json "${DST}/device_descriptions/"
cp "${SRC}/paramset_descriptions/"*.json "${DST}/paramset_descriptions/"

DEV_COUNT=$(find "${DST}/device_descriptions" -name '*.json' | wc -l | tr -d ' ')
PS_COUNT=$(find "${DST}/paramset_descriptions" -name '*.json' | wc -l | tr -d ' ')

echo "copied ${DEV_COUNT} device descriptions and ${PS_COUNT} paramset descriptions from ${SRC}"
