# OLM upgrade and catalog strategy

## Tech-preview to stable upgrade

The upgrade strategy is in-place. An existing tech-preview subscription can move
to the `stable` channel and keep its custom resources. OLM does not move a
subscription between channels automatically.

The stable CSV and catalog entry replace
`model-validation-operator.v0.1.1`. This is the tech-preview CSV's
`metadata.name`; its `spec.version` is `0.1.1-techpreview`. The suffix is not
part of the CSV name. The exact `replaces` edge is used because the installed
tech-preview CSV is a known predecessor; no broad `skipRange` is needed.

Build and publish the stable bundle and FBC catalog, then update the CatalogSource
used by the Subscription to serve that catalog. Wait for the CatalogSource to be
ready, then patch the Subscription in its namespace:

```sh
oc patch subscription <subscription-name> -n <namespace> --type merge \
  -p '{"spec":{"channel":"stable"}}'
```

OLM then installs the stable CSV over the existing operator. Do not uninstall the
operator or delete its resources during this channel change.

Verify that the GA CSV reaches `Succeeded` and that the ModelValidation and
TelemetryConfig resources present before the update are still present afterward:

```sh
oc get csv model-validation-operator.v1.0.0 -n <operator-namespace> \
  -o jsonpath='{.status.phase}'
oc get modelvalidations.ml.sigstore.dev,telemetryconfigs.ml.sigstore.dev -A
```

The automated OLM upgrade test covers stable-to-stable upgrades using the legacy
`opm index add` catalog. Perform the tech-preview-to-stable check manually against
the published FBC catalog before the GA release, and verify that the existing
custom resources remain after the Subscription moves to `stable`.

## Upstream version scheme

The current upstream `VERSION` is `1.0.0`; the GA CSV name and its `spec.version`
also use `1.0.0`. The operator version controls the bundle and catalog image tags.
The previous tech-preview release used CSV name
`model-validation-operator.v0.1.1` and `spec.version` `0.1.1-techpreview`.

This upstream repository does not define an RHTAS `release-version`. Downstream
release pipelines may track an RHTAS product version independently, but that value
does not change the upstream operator, CSV, bundle, or catalog versions.

## FBC catalog

`catalog/template.yaml` defines the package, channel, bundle image, and upgrade
edge. `make catalog-build` renders and validates the template with `opm`,
generates the catalog Dockerfile, and builds
`ghcr.io/sigstore/model-validation-operator-catalog:v$(VERSION)`. The referenced
bundle must already be published and pullable from GHCR. `make catalog-push`
builds and pushes the catalog image.
