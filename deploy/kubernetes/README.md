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
kubectl apply \
  -f deploy/kubernetes/configmap.yaml \
  -f deploy/kubernetes/api-deployment.yaml \
  -f deploy/kubernetes/api-hpa.yaml \
  -f deploy/kubernetes/worker-deployment.yaml \
  -f deploy/kubernetes/worker-hpa.yaml \
  -f deploy/kubernetes/service.yaml \
  -f deploy/kubernetes/ingress.yaml
kubectl -n gophprofile rollout status deployment/gophprofile-api
kubectl -n gophprofile rollout status deployment/gophprofile-worker
```

Add a `spec.tls` section and a certificate Secret to support TLS termination at
the Ingress.

## Autoscaling and load balancing

Two HorizontalPodAutoscalers are installed:

| Workload             | Replicas | Metrics                           |
| -------------------- | -------- | --------------------------------- |
| `gophprofile-api`    | 2-10     | 70% CPU or 80% memory utilization |
| `gophprofile-worker` | 1-10     | 70% CPU utilization               |

Both Deployments define resource requests, which HPA uses as the utilization
baseline. Scale-up reacts immediately. Scale-down waits five minutes and then
removes at most one pod per minute to reduce replica-count flapping.

## Health endpoints

- `GET /health/live` checks that the API process can answer HTTP requests.
- `GET /health/ready` checks PostgreSQL and the RabbitMQ connection.
