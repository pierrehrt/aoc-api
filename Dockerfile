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

# Railway passes these; they end up in /health via internal/version.
ARG VERSION=dev
ARG COMMIT=none

# CGO_ENABLED=0 makes a static binary, which is what lets the final stage be scratch.
# -trimpath keeps build-machine paths out of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
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
