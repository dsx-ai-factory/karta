#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 NVIDIA Corporation
#
# Install the Karta operator from the local chart, in the arrangement
# KARTA_WEBHOOK_MODE selects. Run standalone against the current context, or via
# up.sh, which runs it last.
# shellcheck disable=SC2154  # KARTA_* and IMAGE come from global.env via _common.sh
set -euo pipefail
MODULE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "${MODULE_DIR}/operators/_common.sh"

# REPO_ROOT is exported by up.sh; derive it when this script is run on its own.
REPO_ROOT="${REPO_ROOT:-$(cd "${MODULE_DIR}/../.." && pwd)}"

# Names the chart renders (charts/karta/templates/_helpers.tpl). The service name is
# also the serving-cert SAN, so the Certificate below has to match it.
KARTA_WEBHOOK_SERVICE="karta-operator-webhook"
KARTA_WEBHOOK_SECRET="karta-operator-webhook-cert"
KARTA_WEBHOOK_CONFIGS="mutatingwebhookconfiguration/karta-operator-mutating validatingwebhookconfiguration/karta-operator-validating"
KARTA_WEBHOOK_CERT="karta-webhook-cert"

# The chart ships no Issuer or Certificate, so provisionMode=manual is only installable
# if the caller supplies them. This has to run before the helm install rather than from
# a test: controller-runtime reads the serving cert at startup, so a pod that starts
# without the Secret crashloops instead of waiting for it.
install_certificate() {
  kubectl create namespace "${KARTA_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl apply -f - >/dev/null <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: karta-selfsigned
  namespace: ${KARTA_NAMESPACE}
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ${KARTA_WEBHOOK_CERT}
  namespace: ${KARTA_NAMESPACE}
spec:
  secretName: ${KARTA_WEBHOOK_SECRET}
  issuerRef:
    name: karta-selfsigned
    kind: Issuer
  dnsNames:
    - ${KARTA_WEBHOOK_SERVICE}.${KARTA_NAMESPACE}.svc
    - ${KARTA_WEBHOOK_SERVICE}.${KARTA_NAMESPACE}.svc.cluster.local
EOF
  kubectl wait --for=condition=Ready "certificate/${KARTA_WEBHOOK_CERT}" \
    -n "${KARTA_NAMESPACE}" --timeout=120s
}

# Runs after the helm install, since cainjector only acts on configs that already carry
# the annotation. Nothing else gates on it: auto mode's rotator writes the caBundle
# before the operator reports ready, but in manual mode cainjector is asynchronous, so
# without this the first admission call can fail x509 against an empty caBundle.
wait_for_ca_injection() {
  local target ca
  for target in ${KARTA_WEBHOOK_CONFIGS}; do
    ca=""
    for _ in $(seq 1 60); do
      ca="$(kubectl get "${target}" -o jsonpath='{.webhooks[0].clientConfig.caBundle}' 2>/dev/null || true)"
      [ -n "${ca}" ] && break
      sleep 2
    done
    if [ -z "${ca}" ]; then
      fail "cainjector did not populate caBundle on ${target} within 120s"
      exit 1
    fi
  done
}

main() {
  echo "==> Karta operator (webhook: ${KARTA_WEBHOOK_MODE})"
  kubectl apply --server-side -f "${REPO_ROOT}/charts/karta/crds/"

  # One --set list per route, so the rest of the install cannot drift between them.
  local webhook_values=()
  case "${KARTA_WEBHOOK_MODE}" in
    auto)
      webhook_values=(--set webhook.enabled=true --set webhook.cert.provisionMode=auto)
      ;;
    cert-manager)
      install_certificate
      webhook_values=(
        --set webhook.enabled=true
        --set webhook.cert.provisionMode=manual
        # cainjector reads this and writes the issuing CA into both caBundles.
        --set-string "webhook.cert.annotations.cert-manager\.io/inject-ca-from=${KARTA_NAMESPACE}/${KARTA_WEBHOOK_CERT}"
      )
      ;;
    disabled)
      webhook_values=(--set webhook.enabled=false)
      ;;
    *)
      # up.sh validates this too, so --list catches a typo without provisioning.
      echo "error: unknown KARTA_WEBHOOK_MODE '${KARTA_WEBHOOK_MODE}' (want: auto, cert-manager, disabled)" >&2
      exit 2
      ;;
  esac

  helm upgrade -i karta "${REPO_ROOT}/charts/karta" -n "${KARTA_NAMESPACE}" --create-namespace \
    --set image.repository="${IMAGE%:*}" --set image.tag="${IMAGE##*:}" \
    --set resources.limits.memory="${KARTA_OPERATOR_MEMORY}" \
    "${webhook_values[@]}" >/dev/null
  rollout_wait "${KARTA_NAMESPACE}" deploy/karta-operator 120s
  [ "${KARTA_WEBHOOK_MODE}" = "cert-manager" ] && wait_for_ca_injection
  # Explicit, or the test above returns 1 on the other two routes and set -e aborts.
  return 0
}

main "$@"
