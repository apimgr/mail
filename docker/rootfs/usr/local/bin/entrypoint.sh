#!/usr/bin/env bash
set -euo pipefail

APP_BIN=/usr/local/bin/mail

export TZ="${TZ:-America/New_York}"
export CONFIG_DIR="${CONFIG_DIR:-/config/mail}"
export DATA_DIR="${DATA_DIR:-/data/mail}"
export CACHE_DIR="${CACHE_DIR:-/data/mail/cache}"
export LOG_DIR="${LOG_DIR:-/data/log/mail}"
export BACKUP_DIR="${BACKUP_DIR:-/data/backups/mail}"
export DATABASE_DIR="${DATABASE_DIR:-/data/db/sqlite}"
export LISTEN="${LISTEN:-[::]}"
export PORT="${PORT:-80}"

exec "$APP_BIN" "$@"
