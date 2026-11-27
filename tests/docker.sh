#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT=$(git rev-parse --show-toplevel)
PROJECT_NAME=$(git remote get-url origin 2>/dev/null | sed -E 's|.*/([^/]+)(\.git)?$|\1|' | sed 's/\.git$//')
PROJECT_ORG=$(git remote get-url origin 2>/dev/null | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$|\1|')
PROJECT_NAME=${PROJECT_NAME:-$(basename "$PROJECT_ROOT")}
PROJECT_ORG=${PROJECT_ORG:-$(basename "$(dirname "$PROJECT_ROOT")")}
BIN_DIR=${BINDIR:-binaries}
RELEASE_DIR=${RELDIR:-releases}
if [[ "$BIN_DIR" = /* ]]; then
	BIN_PATH=$BIN_DIR
else
	BIN_PATH="$PROJECT_ROOT/$BIN_DIR"
fi
SERVER_BINARY="$BIN_PATH/$PROJECT_NAME"
CLIENT_BINARY="$BIN_PATH/$PROJECT_NAME-cli"

if ! command -v docker >/dev/null 2>&1; then
	echo "Docker is required for the binary smoke suite." >&2
	exit 127
fi

if [[ ! -x "$SERVER_BINARY" || ! -x "$CLIENT_BINARY" ]]; then
	(cd "$PROJECT_ROOT" && make build BINDIR="$BIN_DIR" RELDIR="$RELEASE_DIR")
fi

container_suffix=$(printf '%08x' "$RANDOM$RANDOM")
container_name="$PROJECT_NAME-$container_suffix"

# Variables in the container script are expanded by its shell.
# shellcheck disable=SC2016
timeout -k 30 3600 docker run --rm --no-healthcheck \
	--name "$container_name" \
	--memory=2g --cpus=2 \
	--volume "$BIN_PATH:/app:ro" \
	--env PROJECT_NAME="$PROJECT_NAME" \
	--env PROJECT_ORG="$PROJECT_ORG" \
	alpine:latest sh -ec '
    apk add --no-cache bash curl file jq tzdata ca-certificates >/dev/null
    binary="/app/$PROJECT_NAME"
    client="/app/$PROJECT_NAME-cli"
    "$binary" --version
    "$binary" --help >/dev/null
    "$client" --version
    "$client" --help >/dev/null
    cp "$binary" /tmp/renamed-server
    chmod 755 /tmp/renamed-server
    /tmp/renamed-server --help 2>&1 | grep -q renamed-server

    export CONFIG_DIR="/tmp/apimgr/$PROJECT_NAME/config"
    export DATA_DIR="/tmp/apimgr/$PROJECT_NAME/data"
    export CACHE_DIR="/tmp/apimgr/$PROJECT_NAME/cache"
    export LOG_DIR="/tmp/apimgr/$PROJECT_NAME/log"
    export BACKUP_DIR="/tmp/apimgr/$PROJECT_NAME/backups"
    export DATABASE_DIR="/tmp/apimgr/$PROJECT_NAME/db"
    export LISTEN=127.0.0.1
    export PORT=64500
    mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$CACHE_DIR" "$LOG_DIR" "$BACKUP_DIR" "$DATABASE_DIR"
    "$binary" >/tmp/mail-server.log 2>&1 &
    server_pid=$!
    cleanup() {
      kill "$server_pid" 2>/dev/null || true
      wait "$server_pid" 2>/dev/null || true
    }
    trap cleanup EXIT INT TERM

    ready=false
    for attempt in $(seq 1 40); do
      if curl --silent --fail -H "Accept: application/json" \
        http://127.0.0.1:64500/api/v1/server/healthz >/tmp/health.json; then
        ready=true
        break
      fi
      kill -0 "$server_pid" 2>/dev/null || break
      sleep 0.5
    done
    [ "$ready" = true ] || { echo "server did not become healthy" >&2; exit 1; }
    grep -q "healthy" /tmp/health.json
    "$binary" --status
    if ! "$client" --server http://127.0.0.1:64500 status --output json | jq -e . >/dev/null; then
      echo "client status command did not return JSON" >&2
      exit 1
    fi

    for page in / /server/about /server/privacy /server/contact /server/help \
      /server/help/mcp /server/terms /server/dpo /server/security \
      /server/security/policy /server/security/thanks /server/preferences \
      /server/preferences/sync \
      /server/docs/swagger /server/docs/graphql; do
      status=$(curl --silent --show-error -o /tmp/page.html -w "%{http_code}" \
        -H "Accept: text/html" "http://127.0.0.1:64500$page")
      case "$status" in
        200|503)
          if ! grep -q "<main id=\"main-content\">" /tmp/page.html; then
            echo "page $page (HTTP $status) is missing the shared main-content shell" >&2
            exit 1
          fi
          ;;
        *) echo "unexpected status $status for $page" >&2; exit 1 ;;
      esac
    done

    curl --silent --show-error --fail -H "Accept: text/html" \
      http://127.0.0.1:64500/server/healthz | grep "<main id=\"main-content\">" >/dev/null
    curl --silent --show-error --fail -H "Accept: text/plain" \
      http://127.0.0.1:64500/server/healthz | grep -i healthy >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/api/v1/server/about | jq -e . >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/static/css/app.css | grep "color-scheme" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/robots.txt | grep "Sitemap:" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/sitemap.xml | grep "<urlset" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/feed.xml | grep "<feed" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/rss.xml | grep "<rss" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/.well-known/security.txt | grep "Contact:" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/.well-known/llms.txt | grep "# Mail" >/dev/null
    curl --silent --show-error --fail http://127.0.0.1:64500/api/v1/server/preferences/sync | jq -e ".data.url and .data.code" >/dev/null
    curl --silent --show-error --fail -H "Accept: text/html" http://127.0.0.1:64500/ | grep "id=\"cookie-consent\"" >/dev/null

    echo "Docker smoke tests passed for $PROJECT_NAME ($PROJECT_ORG)."
  '
