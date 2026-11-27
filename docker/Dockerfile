FROM casjaysdev/go:latest AS builder

ARG TARGETARCH
ARG VERSION=devel
ARG BUILD_EPOCH
ARG COMMIT_ID
ARG OFFICIAL_SITE

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go generate ./src/graphql && \
    INTERNAL_NAME="$(sed -n 's/^internal_name:[[:space:]]*//p' IDEA.md | head -n1 | tr -d '[:space:]')" && \
    test -n "$INTERNAL_NAME" && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -buildvcs=false \
    -trimpath \
    -ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.CommitID=${COMMIT_ID}' -X 'main.BuildEpoch=${BUILD_EPOCH}' -X 'main.OfficialSite=${OFFICIAL_SITE}' -X 'main.internalName=${INTERNAL_NAME}'" \
    -o /app/binary/mail ./src

FROM alpine:latest

ARG VERSION=devel
ARG BUILD_DATE
ARG BUILD_EPOCH
ARG COMMIT_ID

RUN apk add --no-cache git curl bash tini tor i2pd tzdata ca-certificates

COPY --from=builder /app/binary/mail /usr/local/bin/mail
COPY docker/rootfs/ /
COPY docker/Dockerfile /root/Dockerfile
RUN chmod 755 /usr/local/bin/mail /usr/local/bin/entrypoint.sh

ENV TZ=America/New_York \
    DATABASE_DIR=/data/db/sqlite

EXPOSE 80
STOPSIGNAL SIGRTMIN+3
HEALTHCHECK --start-period=10m --interval=5m --timeout=15s --retries=3 \
    CMD /usr/local/bin/mail --status || exit 1
ENTRYPOINT ["tini", "-p", "SIGTERM", "--", "/usr/local/bin/entrypoint.sh"]
