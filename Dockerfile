# wa server image (Go binary only; the frontend is not included).
#
# Run from WORKDIR /app, where `wa --env production` finds the committed
# .env.production. Mount deployment secrets as /app/.env.production.local and
# persistent state (feedback/usage SQLite, admin token) at /data. See
# DEVELOPING.md "Container image".

FROM golang:1.27-trixie AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION is what `wa version` prints; .git is not in the build context, so
# pass it with `--build-arg VERSION=$(git describe --tags --always --long --dirty)`.
# The image workflow passes the tag. When unset, `wa version` prints "devel".
ARG VERSION=

# cgo matches the Makefile and run-dev.sh builds: the results service needs
# cgo os/user group lookups for LDAP auth.
RUN CGO_ENABLED=1 go build -trimpath \
    -ldflags "-X github.com/wtsi-hgi/wa/cmd.Version=${VERSION}" -o /out/wa .

FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 wa \
    && useradd --system --uid 10001 --gid 10001 --home-dir /data --no-create-home wa \
    && mkdir -p /app /data \
    && chown 10001:10001 /data

COPY --from=build /out/wa /usr/local/bin/wa
COPY .env.production /app/.env.production

# The mlwh serve admin token (.wa-mlwh-server.token) lives in XDG_STATE_HOME,
# so it persists on the /data volume across restarts.
ENV XDG_STATE_HOME=/data

WORKDIR /app
USER 10001:10001

EXPOSE 8091

ENTRYPOINT ["wa"]
CMD ["--help"]
