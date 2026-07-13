FROM golang:1.26.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build --chown=nonroot:nonroot /out/server /app/server
COPY --from=build --chown=nonroot:nonroot /out/worker /app/worker
COPY --chown=nonroot:nonroot web /app/web

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/server"]
