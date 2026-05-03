# E2E overlay

This overlay relaxes two security defaults so the operator can drive a
real `frps` Docker container as part of `make test-e2e`:

- Mounts `/var/run/docker.sock` from the host into the manager Pod so
  the LocalDocker provisioner can talk to the host Docker daemon.
- Sets PodSecurity admission on `frp-operator-system` to `baseline`
  (the default overlay uses `restricted`).

**Do not deploy this overlay outside of e2e.** The `restricted` PSA
on `config/default` is the production default and stays unchanged.
