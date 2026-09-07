<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 NVIDIA Corporation -->

# FIPS 140-3

The Karta operator image is built with Go's native [FIPS 140-3
support](https://go.dev/doc/security/fips140) (`GOFIPS140=v1.0.0`), so all
`crypto/*` operations are served by the CMVP-validated Go Cryptographic Module.
This is a single image: the FIPS module is always linked in, and the
`fipsMode` chart value only controls how strictly it is enforced at runtime.
There is no separate `-fips` image variant.

## Setting the mode

```yaml
fipsMode: "off"
```

`fipsMode` sets `GODEBUG=fips140=<mode>` on the operator container. Valid
values:

- `off` (default) - the FIPS module is not used.
- `on` - the FIPS module is used and runs its startup self-tests, but
  non-approved algorithms are still allowed (advisory mode).
- `only` - non-approved algorithms are rejected. See below before using this
  in production.

```sh
helm upgrade --install karta oci://ghcr.io/run-ai/karta/karta \
  -n karta-system --create-namespace --set fipsMode=only
```

## `only` mode can panic at runtime

With `fips140=only`, the Go FIPS 140-3 module refuses any non-approved
cryptographic algorithm at the call site as a panic, not a graceful error, per
the [`GODEBUG=fips140` option
docs](https://go.dev/doc/security/fips140#the-fips140-godebug-option). If any
code path in the operator's dependency tree (including transitive TLS/crypto
usage) reaches a non-approved algorithm, the pod crashes instead of degrading.
Using `only` is the caller's responsibility: test it against your cluster's
actual configuration before relying on it in production.

The operator's own crypto usage was tested under `fips140=only` against a real
cluster: client-go's TLS connection to the API server, and the webhook's
serving certificate bootstrap (RSA-2048 + `x509.CreateCertificate`, via
`open-policy-agent/cert-controller`). Neither required disabling the default
`X25519MLKEM768` TLS 1.3 hybrid curve (`GODEBUG=tlsmlkem=0`), a workaround
some other Go services need under `fips140=only` because the curve's
implementation calls a non-approved plain X25519 primitive internally (see
[golang/go#78298](https://github.com/golang/go/issues/78298) and
[kubernetes/kubernetes#133743](https://github.com/kubernetes/kubernetes/issues/133743)).
If a future Kubernetes or Go version changes that negotiation and the
operator starts failing outbound TLS handshakes under `fips140=only`, set
`GODEBUG=fips140=only,tlsmlkem=0` via `extraArgs` or a values override.

## Testing locally

```sh
make e2e-up FIPS_MODE=only
```

Provisions the full e2e cluster (kind, cert-manager, fake-gpu-operator, all
workload operators) with the Karta operator running under
`GODEBUG=fips140=only`. See [`hack/e2e/README.md`](../hack/e2e/README.md).
