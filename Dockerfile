# The dependencies are downloaded before the source is copied, so a
# change to the code does not re-download the module cache.
FROM golang:1.27.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# A static binary: the runtime image has no libc to link against, and
# -trimpath keeps the build reproducible.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# Certificates are the whole reason this is not scratch: the Cloud API
# and the Anthropic API are both HTTPS, and Neon's connection is TLS.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 despensa

COPY --from=build /out/ /app/

USER despensa
WORKDIR /app

ENV APP_ENV=prod PORT=8080
EXPOSE 8080

# The API, never the migration: the schema is applied by cmd/migrate as
# a release step, so a deploy that would break the schema fails before
# the new machines take traffic.
ENTRYPOINT ["/app/api"]
