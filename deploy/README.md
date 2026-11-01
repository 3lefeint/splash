# Deploy

Kustomize manifests for the backend (including the embedded web build).

- `base` - Deployment (one replica, in-memory live state), Service, init container `migrate`, Ingress (class `nginx`)
- `overlays/stg` - names suffixed `-stg`, image tag `master`, own Secrets, Ingress class `public`
- `overlays/prod` - unsuffixed names, image tag `latest` (set by release tags), Ingress class `public`

Both overlays run side by side in the namespace `splash`; the `env` label keeps their selectors apart.

Render with `kubectl kustomize deploy/overlays/prod` or `.../stg`.

Postgres is external. These Secrets must exist in the namespace before the first start:

| Secret                 | Key     | Content                                         |
| ---------------------- | ------- | ----------------------------------------------- |
| `splash-database`      | `url`   | Postgres URL (prod), role `splash_app`          |
| `splash-migrate`       | `url`   | Postgres URL (prod), role `splash`              |
| `splash-agent`         | `token` | Shared token the prod agents send               |
| `splash-stg-database`  | `url`   | Postgres URL (staging), role `splash_app`          |
| `splash-stg-migrate`   | `url`   | Postgres URL (staging), role `splash`              |
| `splash-stg-agent`     | `token` | Shared token the staging agents send               |
| `splash-access`        | `team-domain`, `audience`, `admin-emails`, `secret-key`, `public-url` | Prod: Cloudflare Access team domain and application audience tag (required), administrator addresses (`admin-emails`, optional), the key that seals the Cloudflare secrets of the members (`secret-key`, 32 random bytes as base64, optional: without it no secret is stored) and the address members reach the backend at (`public-url`, e.g. `https://splash.example.com`, optional: derived from the request without it) |
| `splash-stg-access`    | same keys | Staging: optional                                 |

The prod Pods do not start without the secret `splash-access` and its keys `team-domain` and `audience`, so prod can
never run with browser authentication off. Staging and dev do not need a secret: without the variables the backend starts
with authentication off and logs a warning. Keep the values out of the repository.

Two database roles are used. `splash` owns the tables and runs the migrations in the init container `migrate`
(`splash migrate`, before every start of the backend). `splash_app` is what the backend connects as; it needs
`CONNECT` on the database and `USAGE` on the schema plus the table and sequence rights below, granted once per
database by the owner role:

```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO splash_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO splash_app;
ALTER DEFAULT PRIVILEGES FOR ROLE splash IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO splash_app;
ALTER DEFAULT PRIVILEGES FOR ROLE splash IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO splash_app;
```

The default privileges cover the tables and sequences that later migrations create (they run as `splash`), so the
grants do not have to be repeated after a migration.

The backend pool holds 10 connections; `SPLASH_DB_MAX_CONNS` changes that.

The ingress hosts are placeholders (`splash.example.com`, `splash-stg.example.com`); change them in your local overlay.

Both environments are served by one Ingress of class `public`, the controller behind the tunnel; there is no LAN host.
Browsers and agents reach them through Cloudflare Access, and the backend rejects a request that did not pass it.

| Ingress | Class | Placeholder host |
| ------- | ----- | ---------------- |
| `splash` (prod) | `public` | `splash.example.com` |
| `splash-stg` (staging overlay) | `public` | `splash-stg.example.com` |

The real host names are set in the local overlay (below). For local development use the dev backend without
authentication (`scripts/dev.sh`).

The ingress annotations assume ingress-nginx; the agent WebSocket and the SSE stream need long read timeouts
and no response buffering, also on any proxy in front of it.

## Local overlay

Keep the real host names and cluster specifics out of the repository: create an overlay outside it that references
`deploy/overlays/prod` (or `stg`) and patches the Ingress.

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

For the staging overlay the Ingress is already suffixed: target the name `splash-stg` instead.

Apply it with `kubectl apply -k <overlay dir>`, then check `kubectl -n splash rollout status deploy/splash` and
`https://<host>/healthz`. The tables are created by the init container `migrate` on the first start.

TLS is not handled in the cluster; terminate it in front of the ingress. Agents then connect with
`wss://<host>/v1/agent/ws`; without TLS use `ws://`.

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
