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

run_sql_query() {
  _query="$1"
  if command -v psql >/dev/null 2>&1; then
    psql "$DB_URL" -Atqc "$_query"
    return
  fi
  if command -v docker >/dev/null 2>&1 && [ -f "$ROOT_DIR/docker-compose.yml" ]; then
    cd "$ROOT_DIR"
    docker compose exec -T postgres psql "$DB_URL_IN_CONTAINER" -Atqc "$_query"
    return
  fi
  echo "psql not found and docker compose unavailable. Install PostgreSQL client tools or start Docker." >&2
  exit 1
}

mark_applied() {
  _name="$1"
  run_sql_query "INSERT INTO schema_migrations (name) VALUES ('$_name') ON CONFLICT (name) DO NOTHING;"
}

is_applied() {
  _name="$1"
  _result="$(run_sql_query "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = '$_name');")"
  [ "$_result" = "t" ]
}

table_exists() {
  _table="$1"
  _result="$(run_sql_query "SELECT EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = '$_table'
  );")"
  [ "$_result" = "t" ]
}

column_exists() {
  _table="$1"
  _column="$2"
  _result="$(run_sql_query "SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = '$_table' AND column_name = '$_column'
  );")"
  [ "$_result" = "t" ]
}

ensure_migrations_table() {
  run_sql_query "
    CREATE TABLE IF NOT EXISTS schema_migrations (
      name VARCHAR(255) PRIMARY KEY,
      applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
  " >/dev/null
}

baseline_existing_database() {
  if table_exists "users"; then
    mark_applied "001_init.sql"
    mark_applied "002_seed.sql"
  fi

  if column_exists "bookings" "quantity"; then
    mark_applied "003_booking_quantity.sql"
  fi

  if column_exists "audit_logs" "quantity"; then
    mark_applied "004_audit_quantity.sql"
  fi

  if ! table_exists "booking_tickets" &&
     ! column_exists "tickets" "status" &&
     ! column_exists "tickets" "booking_id"; then
    mark_applied "005_drop_legacy_ticket_booking_schema.sql"
  fi
}

ensure_compose_postgres

echo "Running migrations against database..."

ensure_migrations_table
baseline_existing_database

for f in "$SCRIPT_DIR"/*.sql; do
  [ -f "$f" ] || continue
  name="$(basename "$f")"
  if is_applied "$name"; then
    echo "  Skipping $name (already applied)"
    continue
  fi
  echo "  Applying $name..."
  run_sql_file "$f"
  mark_applied "$name"
done

echo "Migrations complete."
