# Deploy

Kustomize manifests for the backend (including the embedded web build).

- `base` - Deployment (one replica, in-memory live state), Service, Ingress
- `overlays/dev` - namespace `splash-dev`, image tag `master`
- `overlays/prod` - namespace `splash`, image tag `latest` (set by release tags)

Render with `kubectl kustomize deploy/overlays/prod`.

Postgres is external. Two Secrets must exist in the target namespace before the first start:

| Secret            | Key     | Content                                              |
| ----------------- | ------- | ---------------------------------------------------- |
| `splash-database` | `url`   | Postgres connection URL (`SPLASH_DATABASE_URL`)      |
| `splash-agent`    | `token` | Shared token the agents send (`SPLASH_AGENT_TOKEN`)  |

The ingress host is a placeholder (`splash.example.com`); change it in the overlay patch. The ingress annotations
assume ingress-nginx; the agent WebSocket and the SSE stream need long read timeouts and no response buffering.

## Local overlay

Keep the real host name and cluster specifics out of the repository: create an overlay outside it that references
`deploy/overlays/prod` and patches the Ingress.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../path/to/splash/deploy/overlays/prod
patches:
  - target: {kind: Ingress, name: splash}
    patch: |-
      - op: replace
        path: /spec/rules/0/host
        value: splash.example.com
```

Apply it with `kubectl apply -k <overlay dir>`, then check `kubectl -n splash rollout status deploy/splash` and
`https://<host>/healthz`. The backend creates its tables on the first start.

### TLS

The base Ingress has no TLS section. With cert-manager, add to the same overlay (issuer name is yours):

```yaml
  - target: {kind: Ingress, name: splash}
    patch: |-
      - op: add
        path: /metadata/annotations/cert-manager.io~1cluster-issuer
        value: my-cluster-issuer
      - op: add
        path: /spec/tls
        value:
          - hosts: [splash.example.com]
            secretName: splash-tls
```

Agents then connect with `wss://<host>/v1/agent/ws`; without TLS use `ws://`.

### Image pull

Packages on ghcr are private by default. Either make the package public or create a pull secret and reference it:

```
kubectl -n splash create secret docker-registry ghcr --docker-server=ghcr.io --docker-username=<user> --docker-password=<token>
```

```yaml
  - target: {kind: Deployment, name: splash}
    patch: |-
      - op: add
        path: /spec/template/spec/imagePullSecrets
        value: [{name: ghcr}]
```

The cluster must also reach Postgres (port 5432) on the database VM.

## Image and CI

`Dockerfile` builds the frontend, embeds it into the Go binary and ships it on a distroless non-root base.
`.github/workflows/ci.yml` runs the backend (with a Postgres service), agent and web tests, pushes
`ghcr.io/<owner>/splash` on pushes to `master` and `v*` tags, and builds `splash-agent.exe` on a Windows runner as a
workflow artifact.
