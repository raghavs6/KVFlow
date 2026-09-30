# Builds kvxfer and kvworker inside Docker so the image does not depend on
# the host's Go. kvxfer stays the entrypoint so existing commands still work;
# start a worker with --entrypoint kvworker, and pass -grpc 0.0.0.0:50051
# -data 0.0.0.0:9000, because its 127.0.0.1 defaults mean the container
# itself and nothing else could reach it.
FROM golang:1.26 AS build
WORKDIR /src
# go.sum pins gRPC's checksums; kvworker won't build without it.
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /kvxfer ./cmd/kvxfer \
    && CGO_ENABLED=0 go build -o /kvworker ./cmd/kvworker

# Debian rather than scratch so tc (iproute2) can shape the link and ping
# can confirm the delay it adds.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 iputils-ping \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /kvxfer /usr/local/bin/kvxfer
COPY --from=build /kvworker /usr/local/bin/kvworker
ENTRYPOINT ["kvxfer"]
