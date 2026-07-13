FROM golang:1.26.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build --chown=65532:65532 /out/server /app/server
COPY --from=build --chown=65532:65532 /out/worker /app/worker
COPY --chown=65532:65532 web /app/web

EXPOSE 8080

USER 65532:65532

ENTRYPOINT ["/app/server"]
