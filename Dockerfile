# Builds kvxfer inside Docker so the image does not depend on the host's Go.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /kvxfer ./cmd/kvxfer

# Debian rather than scratch so tc (iproute2) can shape the link and ping
# can confirm the delay it adds.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 iputils-ping \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /kvxfer /usr/local/bin/kvxfer
ENTRYPOINT ["kvxfer"]
