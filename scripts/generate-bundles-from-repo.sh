#!/bin/bash
# Generate bundle definitions from a Drupal distribution's config sync
#
# Usage: ./scripts/generate-bundles-from-repo.sh <repo-url> <output-dir> [branch]
#
# Example for Islandora:
#   ./scripts/generate-bundles-from-repo.sh \
#       https://github.com/Islandora-Devops/islandora-starter-site.git \
#       ./bundles \
#       main
#
# This script:
# 1. Clones the repo (shallow clone)
# 2. Runs the generate-bundles script to parse Drupal config
# 3. Outputs bundle definitions to the specified directory

set -e

if [ $# -lt 2 ]; then
    echo "Usage: $0 <repo-url> <output-dir> [branch]"
    echo ""
    echo "Example:"
    echo "  $0 https://github.com/Islandora-Devops/islandora-starter-site.git ./bundles main"
    exit 1
fi

REPO="$1"
OUTPUT="$2"
BRANCH="${3:-main}"

TMPDIR=$(mktemp -d)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cleanup() {
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

echo "Cloning $REPO (branch: $BRANCH)..."
git clone --depth 1 --branch "$BRANCH" "$REPO" "$TMPDIR/repo" 2>/dev/null

# Find config/sync directory
CONFIG_SYNC="$TMPDIR/repo/config/sync"
if [ ! -d "$CONFIG_SYNC" ]; then
    echo "Error: config/sync directory not found in repository"
    exit 1
fi

echo "Generating bundle definitions..."
cd "$PROJECT_ROOT"
go run ./scripts/generate-bundles \
    --config-sync "$CONFIG_SYNC" \
    --output "$OUTPUT"

echo ""
echo "Done! Bundle definitions written to $OUTPUT"
