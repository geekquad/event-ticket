#!/bin/sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$ROOT_DIR/.env" ]; then
  # shellcheck disable=SC2046
  export $(grep -v '^#' "$ROOT_DIR/.env" | xargs)
fi

DB_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5433/ticketbooking?sslmode=disable}"
# psql runs inside the container; use port 5432 there (host mapping like 5433 does not apply).
DB_URL_IN_CONTAINER="${DATABASE_URL_IN_CONTAINER:-postgres://postgres:postgres@127.0.0.1:5432/ticketbooking?sslmode=disable}"

# When using Docker (no host psql), ensure the compose postgres service is up before applying files.
ensure_compose_postgres() {
  if command -v psql >/dev/null 2>&1; then
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1 || [ ! -f "$ROOT_DIR/docker-compose.yml" ]; then
    return 0
  fi
  echo "Starting postgres (docker compose)..."
  (cd "$ROOT_DIR" && docker compose up -d postgres)
  _i=0
  while [ "$_i" -lt 60 ]; do
    if (cd "$ROOT_DIR" && docker compose exec -T postgres pg_isready -U postgres -d postgres) >/dev/null 2>&1; then
      return 0
    fi
    _i=$((_i + 1))
    sleep 1
  done
  echo "Postgres did not become ready in time. Check: docker compose logs postgres" >&2
  exit 1
}

# Prefer host psql; otherwise run psql inside the compose postgres service (image includes client).
run_sql_file() {
  _file="$1"
  if command -v psql >/dev/null 2>&1; then
    psql "$DB_URL" -f "$_file"
    return
  fi
  if command -v docker >/dev/null 2>&1 && [ -f "$ROOT_DIR/docker-compose.yml" ]; then
    (cd "$ROOT_DIR" && docker compose exec -iT postgres psql "$DB_URL_IN_CONTAINER" -f -) < "$_file"
    return
  fi
  echo "psql not found and docker compose unavailable. Install PostgreSQL client tools or start Docker." >&2
  exit 1
}

ensure_compose_postgres

echo "Running migrations against database..."

for f in "$SCRIPT_DIR"/*.sql; do
  [ -f "$f" ] || continue
  echo "  Applying $(basename "$f")..."
  run_sql_file "$f"
done

echo "Migrations complete."
