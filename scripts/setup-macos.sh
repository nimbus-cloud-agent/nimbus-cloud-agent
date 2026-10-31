#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
command -v brew >/dev/null || { echo 'Please install Homebrew: https://brew.sh'; exit 1; }
command -v node >/dev/null || brew install node
command -v go >/dev/null || brew install go
if ! brew --prefix postgresql@17 >/dev/null 2>&1; then brew install postgresql@17; fi
pg_bin="$(brew --prefix postgresql@17)/bin"
mkdir -p .local
if [ ! -f .local/postgres/PG_VERSION ]; then "$pg_bin/initdb" -D .local/postgres -U nimbus --auth=trust --encoding=UTF8 --locale=C; fi
if ! "$pg_bin/pg_isready" -h 127.0.0.1 -p 55432 >/dev/null; then "$pg_bin/pg_ctl" -D .local/postgres -l .local/postgres.log -o '-h 127.0.0.1 -p 55432' start; fi
db_exists=$("$pg_bin/psql" -h 127.0.0.1 -p 55432 -U nimbus -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='nimbus'")
if [ "$db_exists" != "1" ]; then "$pg_bin/createdb" -h 127.0.0.1 -p 55432 -U nimbus nimbus; fi
npm ci
npm ci --prefix web
npm run build --prefix web
go build -o bin/nimbus-cloud-agent ./cmd/nimbus
echo 'Ready. Add deepseekkey and e2bhobbykey (or NIMBUS_* env vars), then run: npm start'
