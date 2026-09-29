# Railway builds this. A Dockerfile rather than Railway's Go buildpack, deliberately: the
# buildpack picks its own Go version and can change it under us on a redeploy, and this project
# is touched in bursts months apart. An explicit, pinned build is the one that still works in
# March.
#
# Pinned to match go.mod (go 1.23). Bumping Go means bumping BOTH, in the same commit.

# ---- build ------------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src

# Dependencies first, so a code-only change does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The commit arrives as a build arg when set; internal/version falls back to Railway's runtime
# RAILWAY_GIT_COMMIT_SHA when it is not.
ARG COMMIT=none

# ⭐ The VERSION FILE is the version — the one place a release bumps it (AOC-015). Not a build
# arg: before 0.1.0 a Railway service variable fed `0.0.0-dev` through one, so every release
# would have needed a hand edit in Railway that nothing checks. A value that is not x.y.z fails
# the build, so a broken file can never reach /health — Railway keeps the running deploy.
# CGO_ENABLED=0 makes a static binary, which is what lets the final stage be scratch.
# -trimpath keeps build-machine paths out of the binary.
RUN set -eu; \
    VERSION="$(cat VERSION)"; \
    [ "$(printf '%s\n' "$VERSION" | wc -l)" -eq 1 ] \
      && echo "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
      || { echo "VERSION must be one x.y.z line, got '$VERSION'" >&2; exit 1; }; \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w -X 'github.com/pierrehrt/aoc-api/internal/version.Version=${VERSION}' -X 'github.com/pierrehrt/aoc-api/internal/version.Commit=${COMMIT}'" \
      -o /out/aoc-api ./cmd/api

# ---- run --------------------------------------------------------------------
# scratch, not alpine: this binary needs no shell, no package manager and no libc. What is not
# in the image cannot be exploited in it, and the image is a few MB, so a deploy is fast.
FROM scratch

# TLS roots, because the service will call R2 and Railway's Postgres over TLS. Without these
# every outbound HTTPS call fails with "x509: certificate signed by unknown authority" — and it
# fails at runtime, not at build, which is the worst time to find out.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /out/aoc-api /aoc-api

# Non-root. scratch has no /etc/passwd, so this is a bare uid; the binary needs no home and
# writes nothing to disk.
USER 65532:65532

# Documentation only — Railway assigns $PORT and the server reads it.
EXPOSE 8080

ENTRYPOINT ["/aoc-api"]
