#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 NVIDIA Corporation
#
# Run the controller e2e against the current cluster. Provision first with up.sh;
# this script installs nothing.
# shellcheck disable=SC2154  # KARTA_* come from global.env via _common.sh
set -euo pipefail
MODULE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "${MODULE_DIR}/../operators/_common.sh"

REPO_ROOT="${REPO_ROOT:-$(cd "${MODULE_DIR}/../../.." && pwd)}"
ARTIFACTS="${ARTIFACTS:-${REPO_ROOT}/.artifacts}"
E2E_CONTROLLER_TIMEOUT="${E2E_CONTROLLER_TIMEOUT:-15m}"

# On every exit path, not just failure: a trap that only fires on error is one
# nobody tests.
collect() {
  local rc=$?
  mkdir -p "${ARTIFACTS}"
  group "diagnostics"
  kubectl -n "${KARTA_NAMESPACE}" logs "deploy/${KARTA_FULLNAME}" --all-containers --tail=-1 \
    >"${ARTIFACTS}/${KARTA_FULLNAME}.log" 2>&1 || true
  kubectl -n "${KARTA_NAMESPACE}" describe "deploy/${KARTA_FULLNAME}" \
    >"${ARTIFACTS}/${KARTA_FULLNAME}.describe" 2>&1 || true
  kubectl get kartas -o yaml >"${ARTIFACTS}/kartas.yaml" 2>&1 || true
  kubectl get events -A --sort-by=.lastTimestamp >"${ARTIFACTS}/events.txt" 2>&1 || true
  kubectl get mutatingwebhookconfigurations,validatingwebhookconfigurations -o yaml \
    >"${ARTIFACTS}/webhooks.yaml" 2>&1 || true
  echo "==> diagnostics in ${ARTIFACTS}"
  endgroup
  return "${rc}"
}
trap collect EXIT

# Read the route off the cluster rather than the environment: make test-e2e does not
# take KARTA_WEBHOOK_MODE, so global.env's default would mislabel every run.
args="$(kubectl get "deploy/${KARTA_FULLNAME}" -n "${KARTA_NAMESPACE}" \
  -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || true)"
case "${args}" in
  *webhook-cert-mode=auto*) mode=auto ;;
  *webhook-cert-mode=manual*) mode=cert-manager ;;
  *) mode=disabled ;;
esac
echo "==> controller e2e (webhook: ${mode})"
cd "${REPO_ROOT}/operator"
# -count=1 keeps a previous pass from being replayed from the cache.
go test -tags e2e -count=1 -v -timeout "${E2E_CONTROLLER_TIMEOUT}" ./test/e2e/...
