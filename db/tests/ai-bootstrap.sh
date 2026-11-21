#!/bin/sh
# Run on a disposable, migrated incident instance as its bootstrap administrator.
# Only a generated test role is provisioned; the real AI login is never altered.
set -eu
: "${AI_BOOTSTRAP_SQL:?set the path to db/bootstrap/ai-worker.sql}"
[ "${TEST_DISPOSABLE_AI_BOOTSTRAP:-}" = 1 ] && [ "${PGDATABASE:-}" = cyberguard_incidents ] || exit 2
psql_test() { psql -X -q -v ON_ERROR_STOP=1 "$@"; }
dir=$(mktemp -d)
role=ai_test_$(psql_test -Atc "SELECT replace(gen_random_uuid()::text,'-','')")
created=0
cleanup() {
    if [ "$created" = 1 ]; then
        psql_test -c "DROP OWNED BY $role; DROP ROLE $role;"
    fi
    rm -rf "$dir"
}
trap cleanup EXIT
sed "s/python_incidents/$role/g" "$AI_BOOTSTRAP_SQL" > "$dir/bootstrap.sql"
AI_RUNTIME_PASSWORD=$(psql_test -Atc 'SELECT gen_random_uuid()')
export AI_RUNTIME_PASSWORD

# A precreated recovery login (possibly with the migration's readiness grant)
# must pass, as must an idempotent rerun with all approved ACLs in place.
psql_test -c "CREATE ROLE $role LOGIN NOINHERIT"
created=1
psql_test -c "GRANT EXECUTE ON FUNCTION public.ai_worker_ready() TO $role"
psql_test -f "$dir/bootstrap.sql" >/dev/null
psql_test -f "$dir/bootstrap.sql" >/dev/null

reject() {
    # An expected psql failure closes the transaction and discards the setup.
    # Keep error text private: a provisioning failure could mention SQL.
    if { printf 'BEGIN;\n%s;\n' "$1"; cat "$dir/bootstrap.sql"; } |
        psql_test > "$dir/log" 2>&1; then
        echo "Unexpectedly accepted: $1" >&2
        exit 1
    fi
    grep -Fq "unsuitable existing $role $2" "$dir/log" || {
        echo "Incorrect failure for: $1" >&2
        exit 1
    }
}
attrs='attributes, settings or memberships'
grants='ownership or grants'
reject "ALTER ROLE $role BYPASSRLS" "$attrs"
reject "GRANT pg_read_all_data TO $role" "$attrs"
reject "GRANT $role TO CURRENT_USER" "$attrs"
reject "ALTER ROLE $role SET search_path = public" "$attrs"
reject "CREATE SCHEMA $role AUTHORIZATION $role" "$grants"
reject "GRANT CREATE ON DATABASE cyberguard_incidents TO $role" "$grants"
reject "GRANT SELECT ON assessment_results TO $role" "$grants"
reject "GRANT SELECT (claim_token) ON assessment_jobs TO $role" "$grants"
reject "GRANT UPDATE (status) ON assessment_jobs TO $role" "$grants"
reject "GRANT EXECUTE ON FUNCTION ai_worker_ready() TO $role WITH GRANT OPTION" "$grants"
reject "ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO $role" "$grants"
echo 'AI bootstrap approved-role and refusal checks passed.'
