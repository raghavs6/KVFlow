# Builds kvxfer inside Docker so the image does not depend on the host's Go.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /kvxfer ./cmd/kvxfer

# Debian rather than scratch so later steps can install tc (iproute2).
FROM debian:bookworm-slim
COPY --from=build /kvxfer /usr/local/bin/kvxfer
ENTRYPOINT ["kvxfer"]
