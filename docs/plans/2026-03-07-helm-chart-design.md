# Helm Chart Design — testrun-operator

## Purpose

Package the testrun-operator as a Helm chart published to GHCR as an OCI artifact, so Flux in the
`testrun-operator-deploy` cluster can install and manage it via HelmRelease.

---

## Architecture

- **Chart location:** `testrun-operator/chart/`
- **Image registry:** `ghcr.io/twallac10/testrun-operator` (multi-arch: amd64 + arm64)
- **Chart registry:** `oci://ghcr.io/twallac10/charts` (OCI HelmRepository)
- **CI:** Single GitHub Actions workflow `release.yml` — triggers on push to `main`
- **Secrets:** Created by `task operator:secrets` in the deploy repo Taskfile, not by the chart

---

## Helm Chart Structure

```
chart/
  Chart.yaml                        # name: testrun-operator, version: 0.1.0
  values.yaml                       # image, replicaCount, secretName, ports
  templates/
    deployment.yaml                 # manager Deployment, env from secret
    service.yaml                    # ClusterIP: 8080 (webhook), 8081 (metrics)
    serviceaccount.yaml
    clusterrole.yaml                # from config/rbac/role.yaml
    clusterrolebinding.yaml
    leaderelection-role.yaml        # from config/rbac/leader_election_role.yaml
    leaderelection-rolebinding.yaml
  crds/
    helmreleasetests.yaml           # copied from config/crd/bases/
```

### Secret contract

The chart mounts a pre-existing Secret named `{{ .Values.secretName }}` (default:
`testrun-operator-secrets`) as environment variables:
- `HMAC_SECRET` — Flux webhook HMAC key
- `GITHUB_TOKEN` — GitHub PAT for commit status posting
- `GITHUB_REPO` — target repo for commit statuses (e.g. `twallac10/testrun-operator-deploy`)

The Secret is **not** created by the chart. It must exist before the HelmRelease reconciles.

---

## GitHub Actions CI (`release.yml`)

Triggered on push to `main`. Two jobs:

### job: build-push-image
- `docker/build-push-action` with `platforms: linux/amd64,linux/arm64`
- Tags: `ghcr.io/twallac10/testrun-operator:latest` and `ghcr.io/twallac10/testrun-operator:<sha>`
- Permissions: `packages: write`, `contents: read`

### job: package-push-chart (needs: build-push-image)
- Updates `Chart.yaml` `appVersion` to the commit SHA
- `helm package chart/`
- `helm push testrun-operator-*.tgz oci://ghcr.io/twallac10/charts`
- Login to GHCR via `echo $GITHUB_TOKEN | helm registry login ghcr.io`

---

## Deploy Repo Changes

### `apps/operator/source.yaml`
Switch from suspended HTTP HelmRepository to OCI:
```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
spec:
  type: oci
  url: oci://ghcr.io/twallac10/charts
  # suspend removed
```

### `apps/operator/release.yaml`
Un-suspend, reference real chart:
```yaml
spec:
  suspend: false
  chart:
    spec:
      chart: testrun-operator
      version: "0.1.x"
```

### `Taskfile.yaml` — new task: `operator:secrets`
Creates `testrun-operator-secrets` Secret in `testrun-operator` namespace:
- `HMAC_SECRET` — random hex via `openssl rand -hex 32`
- `GITHUB_TOKEN` — from `gh auth token`
- `GITHUB_REPO` — hardcoded `twallac10/testrun-operator-deploy`

Also add `operator:secrets` as a step in `task up` after namespace is created.

### `test-system/helmreleasetest.yaml`
Fix CRD API group: `testing.testing.platform.io/v1alpha1` (was `testing.platform.io/v1alpha1`).
Also update `spec` to use `cronJobRef` (current CRD field) instead of inline `jobTemplate`.

---

## Decisions

- **OCI chart distribution:** No extra infrastructure, GHCR is already used for image
- **Secrets outside chart:** Taskfile creates them imperatively; chart stays stateless
- **CRD in `crds/` dir:** Helm handles CRD lifecycle (install on first deploy, no delete on uninstall)
- **Single workflow:** Simpler than separate image/chart jobs for this test setup
- **`appVersion` = commit SHA:** Allows tracing which code version is deployed
