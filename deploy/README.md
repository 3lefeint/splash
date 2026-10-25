# Deploy

Kustomize manifests for the backend (including the embedded web build).

- `base` - Deployment, Service, Ingress
- `overlays/dev` - test setup
- `overlays/prod` - event setup

Postgres is external. Database credentials and the agent token are provided as Kubernetes Secrets.
