#!/bin/sh
set -eu

: "${TEST_MYSQL_DSN:?Set TEST_MYSQL_DSN to an isolated MySQL test server}"
: "${TEST_POSTGRES_DSN:?Set TEST_POSTGRES_DSN to an isolated PostgreSQL test server}"
if [ "$#" -eq 0 ]; then
  echo 'Usage: sh .github/scripts/check-databases.sh <released-source-directory> [other-baseline-directory...]' >&2
  exit 2
fi

export GOWORK=off
export CGO_ENABLED=0
export GOMAXPROCS="${GOMAXPROCS:-4}"
export GOFLAGS="${GOFLAGS:--p=4}"
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT HUP INT TERM
export TEST_DATABASE_DIRECTORY="$test_root/sqlite"
run_id="${TEST_DATABASE_RUN_ID:-$(date +%s)}"
candidate=$(pwd)

echo 'Run existing SQLite, MySQL, and PostgreSQL model regressions'
go test -count=1 -v ./model
go test -c -o "$test_root/candidate.test" ./model

run_fixture() {
  TEST_DATABASE_PHASE="$1" TEST_DATABASE_DIALECT="$2" TEST_DATABASE_NAME="$3" \
    "$4" -test.v -test.run '^TestDatabaseMigrationLifecycle$' -test.timeout 5m
}

for dialect in sqlite mysql postgres; do
  database="newapi_matrix_${run_id}_${dialect}_fresh"
  echo "Check fresh startup and repeated migration: $dialect"
  run_fixture seed "$dialect" "$database" "$test_root/candidate.test"
  run_fixture verify "$dialect" "$database" "$test_root/candidate.test"
done

baseline_number=0
for baseline in "$@"; do
  echo "Build released baseline: $baseline"
  cp "$candidate/model/database_migration_test.go" "$baseline/model/database_migration_test.go"
  (cd "$baseline" && go test -c -o "$test_root/baseline.test" ./model)
  for dialect in sqlite mysql postgres; do
    database="newapi_matrix_${run_id}_${dialect}_upgrade${baseline_number}"
    echo "Check upgrade and repeated migration: $baseline -> candidate ($dialect)"
    run_fixture seed "$dialect" "$database" "$test_root/baseline.test"
    run_fixture verify "$dialect" "$database" "$test_root/candidate.test"
  done
  baseline_number=$((baseline_number + 1))
done
