# config/overlays/e2e

Kustomize overlay used by `make test-e2e`. Adds host bind-mounts for
the Docker socket + the shared frps.toml directory, sets
`DOCKER_HOST` / `LOCALDOCKER_*` env, and relaxes the PodSecurityAdmission
labels on `frp-operator-system` so the manager can run as `runAsUser: 0`
to talk to the docker socket. Webhooks are not present in this
overlay — the operator no longer ships any.
