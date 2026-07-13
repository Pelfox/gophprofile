# Kubernetes deployment

## Prepare the image and configuration

Build and publish the image:

```bash
docker build -t ghcr.io/pelfox/gophprofile:<tag> .
docker push ghcr.io/pelfox/gophprofile:<tag>
```

Adjust the public host in `ingress.yaml` and the non-secret storage/telemetry
values in `configmap.yaml`.

Create the application secret from the example and replace every placeholder:

```bash
kubectl apply -f deploy/kubernetes/namespace.yaml
cp deploy/kubernetes/secret.example.yaml deploy/kubernetes/secret.yaml
kubectl apply -f deploy/kubernetes/secret.yaml
```

Run the SQL migrations from `migrations/` against `DATABASE_URL` before the
first rollout, then deploy:

```bash
kubectl apply -k deploy/kubernetes
kubectl -n gophprofile rollout status deployment/gophprofile-api
kubectl -n gophprofile rollout status deployment/gophprofile-worker
```

Add a `spec.tls` section and a certificate Secret to support TLS termination at
the Ingress.

## Health endpoints

- `GET /health/live` checks that the API process can answer HTTP requests.
- `GET /health/ready` checks PostgreSQL and the RabbitMQ connection.
