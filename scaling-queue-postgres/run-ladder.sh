#!/usr/bin/env bash
# Runs the whole experiment: ten steps for each benchmark, in order.
#
# The script only arranges the machine. Every check that decides whether a
# number is recorded lives in the Go binary, so a mistake here fails loudly
# instead of writing a cell labelled for a machine it never ran on.
#
# Stopping and re-running resumes: a step that already holds a valid result is
# skipped.
set -euo pipefail

cd "$(dirname "$0")/.."

PGDATA=${PGDATA_DIR:-/private/tmp/scaling-queue-postgres-pgdata}
PGBIN=${PGBIN:-/opt/homebrew/opt/postgresql@18/bin}
NATIVE_PORT=${NATIVE_PORT:-55444}
NATIVE_DSN="postgres://postgres@127.0.0.1:${NATIVE_PORT}/postgres"
MODES=${MODES:-"ops steady"}
STEPS=${STEPS:-"0 1 2 3 4 5 6 7 8 9"}

# Each step tries these claim-loop counts and keeps whichever reaches furthest.
# A step whose best sits at the top of its list gets one more, more aggressive
# attempt, so a peak is never reported at the edge of what was tried.
WORKERS_SMALL=${WORKERS_SMALL:-"4,8,16"}
WORKERS_LARGE=${WORKERS_LARGE:-"16,32,64"}

total=0
for _ in $MODES; do for _ in $STEPS; do total=$((total + 1)); done; done
done_cells=0
started=$(date +%s)

say() { printf '\n=== %s\n' "$*"; }

eta() {
	done_cells=$((done_cells + 1))
	local elapsed=$(($(date +%s) - started))
	local left=$((total - done_cells))
	if [ "$done_cells" -gt 0 ] && [ "$left" -gt 0 ]; then
		local per=$((elapsed / done_cells))
		local remain=$((per * left))
		printf '=== %d/%d cells, %dm elapsed, about %dm left, finishing near %s\n' \
			"$done_cells" "$total" "$((elapsed / 60))" "$((remain / 60))" \
			"$(date -v+${remain}S '+%H:%M' 2>/dev/null || date -d "+${remain} seconds" '+%H:%M')"
	else
		printf '=== %d/%d cells, %dm elapsed\n' "$done_cells" "$total" "$((elapsed / 60))"
	fi
}

vm_cpus() {
	say "setting the virtual machine to $1 CPUs"
	podman machine stop >/dev/null 2>&1 || true
	podman machine set --cpus "$1" >/dev/null
	podman machine start >/dev/null
}

native_up() {
	say "starting Postgres on the host"
	if [ ! -d "$PGDATA" ]; then
		"$PGBIN/initdb" -D "$PGDATA" -U postgres --auth=trust --no-locale -E UTF8 >/dev/null
	fi
	"$PGBIN/pg_ctl" -D "$PGDATA" -l "$PGDATA/log" \
		-o "-p $NATIVE_PORT -c listen_addresses=127.0.0.1 -c max_connections=1200" \
		start >/dev/null 2>&1 || true
	until "$PGBIN/pg_isready" -h 127.0.0.1 -p "$NATIVE_PORT" -q; do sleep 1; done
}

native_down() { "$PGBIN/pg_ctl" -D "$PGDATA" stop >/dev/null 2>&1 || true; }

for mode in $MODES; do
	for step in $STEPS; do
		workers=$WORKERS_LARGE
		[ "$step" -le 2 ] && workers=$WORKERS_SMALL

		dsn=()
		case "$step" in
		0 | 1 | 2 | 3 | 4 | 5 | 6) vm_cpus 2 ;;
		7) vm_cpus 4 ;;
		8) vm_cpus 8 ;;
		9)
			podman machine stop >/dev/null 2>&1 || true
			native_up
			dsn=(-dsn="$NATIVE_DSN")
			;;
		esac

		say "$mode, step $step, claim loops $workers"
		# bash 3.2 ships on macOS, where an empty array under set -u is an
		# error. The ${x[@]+...} form expands to nothing when the array is empty.
		bazel run //scaling-queue-postgres/cmd/bench -- \
			-mode="$mode" -step="$step" -workers="$workers" -skip-recorded \
			${dsn[@]+"${dsn[@]}"} "$@"
		eta
	done
	native_down
done

say "writing the measured numbers into the documents"
bazel run //scaling-queue-postgres/cmd/report
say "done"
