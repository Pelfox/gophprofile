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
  -f deploy/kubernetes/ingress.yaml \
  -f deploy/kubernetes/network-policies.yaml
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

## Security

The image and both workloads run with the numeric non-root UID/GID `65532`.
Kubernetes also enforces `runAsNonRoot`, the `RuntimeDefault` seccomp profile,
a read-only root filesystem, no privilege escalation, and no Linux
capabilities. A size-limited `emptyDir` keeps `/tmp` writable without making
the container filesystem writable.

`network-policies.yaml` applies default-deny ingress and egress rules, then
allows only:

- API traffic on port `8080` from the `ingress-nginx` and `monitoring`
  namespaces;
- DNS lookups over TCP/UDP port `53`;
- PostgreSQL on `5432`, AMQP/AMQPS on `5672`/`5671`, OTLP HTTP on `4318`, and
  S3-compatible storage on `80`, `443`, or `9000` as required by each process.

The cluster CNI must support Kubernetes NetworkPolicy. Change the namespace
selectors if the Ingress controller or monitoring stack uses different
namespaces, and update the egress ports when dependencies use non-standard
ports. Apply and verify the allow rules before enabling the default-deny policy
in an existing production namespace.

## Health endpoints

- `GET /health/live` checks that the API process can answer HTTP requests.
- `GET /health/ready` checks PostgreSQL and the RabbitMQ connection.
