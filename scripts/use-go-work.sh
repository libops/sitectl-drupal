#!/usr/bin/env bash

set -euo pipefail

SITECTL_PATH="${1:-../sitectl}"
SITECTL_GOMOD="${SITECTL_PATH}/go.mod"

if [[ ! -f "${SITECTL_GOMOD}" ]]; then
	rm -f go.work
	echo "Skipping go.work; local sitectl checkout not found at ${SITECTL_PATH}"
	exit 0
fi

GO_LINE="$(grep -E '^go [0-9]+([.][0-9]+)*$' go.mod || true)"
if [[ -z "${GO_LINE}" ]]; then
	echo "Unable to read Go directive from go.mod"
	exit 1
fi
{
	echo "${GO_LINE}"
	echo
	echo "use ("
	echo "    ."
	echo "    ${SITECTL_PATH}"
	echo ")"
} > go.work

echo "Wrote go.work using local sitectl checkout at ${SITECTL_PATH}"
