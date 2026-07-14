# gophprofile

`gophprofile` is an avatar upload and processing service written in Go. The
HTTP API stores avatar metadata in PostgreSQL and original files in
S3-compatible object storage. RabbitMQ connects the API to a background worker
that creates `100x100` and `300x300` JPEG thumbnails and processes deletions.

## Features

- Upload JPEG, PNG, and WebP avatars up to 10 MiB.
- Retrieve an original avatar, its metadata, or a user's latest avatar.
- Generate thumbnails asynchronously with RabbitMQ-backed jobs.
- Drain the active worker job gracefully during Kubernetes termination.
- Soft-delete metadata and remove related objects asynchronously.
- Export Prometheus metrics and OpenTelemetry traces.
- Expose Kubernetes liveness and readiness checks.
- Rate-limit API traffic and avatar uploads with sliding-window counters.
- Deploy the API and worker with Docker, Kubernetes HPA, and load balancing.
- Run as non-root with restricted security contexts and NetworkPolicy rules.

## API

| Method   | Path                                  | Description                                       | Required header |
| -------- | ------------------------------------- | ------------------------------------------------- | --------------- |
| `POST`   | `/api/v1/avatars`                     | Upload an avatar as multipart field `avatar_file` | `X-User-ID`     |
| `GET`    | `/api/v1/avatars/{avatarID}`          | Return the original avatar                        | -               |
| `GET`    | `/api/v1/avatars/{avatarID}/metadata` | Return avatar metadata and processing state       | -               |
| `DELETE` | `/api/v1/avatars/{avatarID}`          | Delete an owned avatar                            | `X-User-ID`     |
| `GET`    | `/api/v1/users/me/avatar`             | Return the user's latest avatar                   | `X-User-ID`     |

`X-User-ID` and `avatarID` values must be valid UUIDs.

## Rate limiting

All `/api/v1` endpoints are limited to 10 requests per second per client IP.
Avatar uploads have an additional per-IP limit of 5 requests per minute.
Rejections are exported by the existing HTTP counter as
`gophprofile_http_requests_total{status="429"}`.

## Health and observability

| Path            | Purpose                                        |
| --------------- | ---------------------------------------------- |
| `/health/live`  | Confirms that the HTTP process is responding.  |
| `/health/ready` | Checks PostgreSQL and the RabbitMQ connection. |
| `/health`       | Compatibility alias for the readiness check.   |
| `/metrics`      | Exposes Prometheus metrics.                    |

Liveness deliberately does not check external dependencies, avoiding process
restarts during a temporary database or broker outage.
