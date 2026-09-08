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

`fipsMode` sets `GODEBUG=fips140=<mode>` on the operator container. This is a
runtime switch, not a build-time one: `GOFIPS140=v1.0.0` always links the FIPS
module into the operator image, regardless of `fipsMode`. Valid values:

- `off` (default) - FIPS mode disabled at runtime; the module is present in
  the binary but not engaged, and no self-tests run.
- `on` - the FIPS module is used and runs its startup self-tests, but
  non-approved algorithms are still allowed (advisory mode).
- `only` - non-approved algorithms are rejected. See below before using this
  in production.

```sh
helm upgrade --install karta oci://ghcr.io/run-ai/karta/karta \
  -n karta-system --create-namespace --set fipsMode=only
```

## `only` mode is a testing aid, not a production mode

Per the [`GODEBUG=fips140` option
docs](https://go.dev/doc/security/fips140#the-fips140-godebug-option),
`fips140=only` is a best-effort diagnostic for testing, assessment, and
debugging. Upstream Go explicitly does not recommend it for production. When a
non-approved cryptographic algorithm is used, the Go FIPS 140-3 module can
return an error or panic at the call site, depending on the code path; a
panic crashes the pod instead of degrading gracefully. Using `only` is the
caller's responsibility: test it against your cluster's actual configuration
before relying on it, and do not treat it as a substitute for `on` in
production.

The operator's own crypto usage was tested under `fips140=only` against a
real cluster: client-go's TLS connection to the API server worked cleanly.
