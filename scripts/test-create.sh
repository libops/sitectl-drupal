#!/usr/bin/env bash

set -euo pipefail
set -x

export TERM="${TERM:-dumb}"

PLUGIN_NAME="drupal"
PLUGIN_BINARY="sitectl-drupal"
SITE_DIR_NAME="drupal"
CREATE_DEFINITION="${CREATE_DEFINITION:-default}"
CREATE_ARGS="${CREATE_ARGS:-}"
SITECTL_CONTEXT="${SITECTL_CONTEXT:-integration-test}"

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &>/dev/null && pwd)"

if [ -n "${SITECTL_TMP_PARENT:-}" ]; then
	TMP_PARENT="${SITECTL_TMP_PARENT}"
elif [ -n "${GITHUB_WORKSPACE:-}" ]; then
	TMP_PARENT="${GITHUB_WORKSPACE}"
else
	TMP_PARENT="${HOME}/.tmp"
fi
mkdir -p "${TMP_PARENT}"
TMP_DIR="$(mktemp -d "${TMP_PARENT%/}/${PLUGIN_BINARY}-test.XXXXXX")"
SITECTL_HOME="${TMP_DIR}/home"
BIN_DIR="${TMP_DIR}/bin"
SITE_DIR="${TMP_DIR}/${SITE_DIR_NAME}"
PATH="${BIN_DIR}:${PATH}"
export PATH
mkdir -p "${SITECTL_HOME}"

cleanup() {
	if [ -d "${SITE_DIR}" ] && command -v sitectl >/dev/null 2>&1; then
		HOME="${SITECTL_HOME}" sitectl compose down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

build_plugin() {
	mkdir -p "${BIN_DIR}"
	(
		cd "${REPO_ROOT}" &&
			go build -o "${BIN_DIR}/${PLUGIN_BINARY}" .
	)
	command -v sitectl >/dev/null
	command -v "${PLUGIN_BINARY}" >/dev/null
}

assert_template_lock() {
	local lock="${SITE_DIR}/.libops/template.lock.yaml"
	if [ -L "${lock}" ] || [ ! -f "${lock}" ]; then
		echo "sitectl create did not retain a regular template provenance lock" >&2
		return 1
	fi
	test "$(stat -c '%a' "${lock}")" = "644"
	grep -Fxq 'apiVersion: sitectl.libops.io/v1alpha1' "${lock}"
	grep -Fxq 'kind: TemplateLock' "${lock}"
	grep -Eq '^    commit: [0-9a-f]{40}([0-9a-f]{24})?$' "${lock}"
	grep -Fxq "    repository: https://github.com/libops/${PLUGIN_NAME}" "${lock}"
	grep -Eq '^        digest: sha256:[0-9a-f]{64}$' "${lock}"
	grep -Fxq '    revision: v1.0.0' "${lock}"
	grep -Fxq '    ref: v1.2.1' "${lock}"
}

create_site() {
	local target="${PLUGIN_NAME}/${CREATE_DEFINITION}"
	local extra_args=()
	if [ -n "${CREATE_ARGS}" ]; then
		read -r -a extra_args <<< "${CREATE_ARGS}"
	fi
	extra_args+=(--yolo)

	HOME="${SITECTL_HOME}" sitectl create "${target}" \
		--path "${SITE_DIR}" \
		--type local \
		--checkout-source template \
		--default-context \
		"${extra_args[@]}"
	assert_template_lock
}

run_healthcheck() {
	HOME="${SITECTL_HOME}" sitectl healthcheck
	HOME="${SITECTL_HOME}" sitectl verify --strict
}

main() {
	build_plugin
	create_site
	run_healthcheck
}

main "$@"
