#!/bin/sh
# End-to-end tests: a local relay, the Go CLI and run.sh talking to each other.
#
# Usage: TUBO_JAR=server/target/tubo-server-1.0.0-fat.jar TUBO_BIN=tubo-cli/tubo sh ci/e2e.sh
# Runs run.sh with dash when available, to catch non-POSIX shell code.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
JAR=$(cd "$(dirname "$TUBO_JAR")" && pwd)/$(basename "$TUBO_JAR")
BIN=$(cd "$(dirname "$TUBO_BIN")" && pwd)/$(basename "$TUBO_BIN")
PORT=${PORT:-18080}
SHELL_UNDER_TEST=$(command -v dash || echo sh)

WORK=$(mktemp -d)
export TUBO_SERVER="http://127.0.0.1:$PORT"
unset TUBO_TOKEN || true

SERVER_PID=""
cleanup() {
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
    rm -rf "$WORK"
}
trap cleanup EXIT

fail() {
    echo "FAIL: $*" >&2
    for f in "$WORK"/*.log; do
        [ -f "$f" ] && { echo "--- $f" >&2; cat "$f" >&2; }
    done
    exit 1
}

pass() { echo "ok - $*"; }

# Waits until a log file prints a token after the given prefix, then echoes it.
wait_token() {
    i=0
    while [ $i -lt 100 ]; do
        token=$(sed -n "s/.*TUBO_TOKEN=\([^ ]*\) tubo $2.*/\1/p" "$1" 2>/dev/null | head -1)
        if [ -n "$token" ]; then
            echo "$token"
            return
        fi
        sleep 0.1
        i=$((i + 1))
    done
    fail "no token in $1"
}

run_script() {
    "$SHELL_UNDER_TEST" "$ROOT/run.sh" "$@"
}

# A VAR=value prefix on a shell function leaks into the caller, so the token
# goes through env(1) instead.
run_script_token() {
    token=$1
    shift
    env TUBO_TOKEN="$token" "$SHELL_UNDER_TEST" "$ROOT/run.sh" "$@"
}

PORT=$PORT java -jar "$JAR" > "$WORK/server.log" 2>&1 &
SERVER_PID=$!
i=0
until curl -s "$TUBO_SERVER/" >/dev/null 2>&1; do
    i=$((i + 1))
    [ $i -gt 100 ] && fail "server did not start"
    sleep 0.2
done

mkdir -p "$WORK/src/project/sub" "$WORK/out"
head -c 3000000 /dev/urandom > "$WORK/src/random.bin"
echo "hello" > "$WORK/src/project/a.txt"
head -c 200000 /dev/urandom > "$WORK/src/project/sub/b.bin"

# 1. CLI -> CLI, sender starts
"$BIN" send "$WORK/src/random.bin" 2> "$WORK/send1.log" &
SEND=$!
TOKEN=$(wait_token "$WORK/send1.log" receive)
(cd "$WORK/out" && TUBO_TOKEN=$TOKEN "$BIN" receive < /dev/null 2> "$WORK/recv1.log")
wait $SEND
cmp "$WORK/src/random.bin" "$WORK/out/random.bin" || fail "cli->cli file differs"
grep -q "Integrity verified" "$WORK/recv1.log" || fail "cli->cli MAC not verified"
pass "cli -> cli (sender starts)"

# 2. CLI -> CLI, receiver starts; existing file is not overwritten
(cd "$WORK/out" && "$BIN" receive < /dev/null 2> "$WORK/recv2.log") &
RECV=$!
TOKEN=$(wait_token "$WORK/recv2.log" send)
"$BIN" send "$WORK/src/random.bin" "$TOKEN" 2> "$WORK/send2.log"
wait $RECV
cmp "$WORK/src/random.bin" "$WORK/out/random.bin.1" || fail "receiver-starts transfer differs"
pass "cli -> cli (receiver starts, no overwrite)"

# 3. Directory with compression
"$BIN" send "$WORK/src/project" --compress 2> "$WORK/send3.log" &
SEND=$!
TOKEN=$(wait_token "$WORK/send3.log" receive)
(cd "$WORK/out" && "$BIN" receive "$TOKEN" < /dev/null 2> "$WORK/recv3.log")
wait $SEND
diff -r "$WORK/src/project" "$WORK/out/project" || fail "directory differs"
pass "cli -> cli (directory, --compress)"

# 4. CLI -> run.sh, name taken from the sender
"$BIN" send "$WORK/src/random.bin" 2> "$WORK/send4.log" &
SEND=$!
TOKEN=$(wait_token "$WORK/send4.log" receive)
mkdir -p "$WORK/out4"
(cd "$WORK/out4" && run_script_token "$TOKEN" receive 2> "$WORK/recv4.log")
wait $SEND
cmp "$WORK/src/random.bin" "$WORK/out4/random.bin" || fail "cli->script differs"
pass "cli -> run.sh"

# 5. run.sh -> CLI
mkdir -p "$WORK/out5"
(cd "$WORK/out5" && "$BIN" receive < /dev/null 2> "$WORK/recv5.log") &
RECV=$!
TOKEN=$(wait_token "$WORK/recv5.log" send)
run_script_token "$TOKEN" send "$WORK/src/random.bin" 2> "$WORK/send5.log"
wait $RECV
cmp "$WORK/src/random.bin" "$WORK/out5/random.bin" || fail "script->cli differs"
pass "run.sh -> cli"

# 6. CLI -> run.sh to stdout, token as argument
"$BIN" send - < "$WORK/src/project/a.txt" 2> "$WORK/send6.log" &
SEND=$!
TOKEN=$(wait_token "$WORK/send6.log" receive)
OUT=$(run_script receive "$TOKEN" - 2> "$WORK/recv6.log")
wait $SEND
[ "$OUT" = "hello" ] || fail "stdout transfer got '$OUT'"
pass "cli (stdin) -> run.sh (stdout)"

# 7. Interrupted run.sh sender: the CLI receiver must fail and save nothing,
#    and the key must never appear in the process list.
mkdir -p "$WORK/out7"
(cd "$WORK/out7" && "$BIN" receive < /dev/null 2> "$WORK/recv7.log") &
RECV=$!
TOKEN=$(wait_token "$WORK/recv7.log" send)
KEY=${TOKEN#*-*-}
{ head -c 500000 /dev/urandom; sleep 8; } | run_script_token "$TOKEN" send - 2> "$WORK/send7.log" &
sleep 2
if ps -eo args= 2>/dev/null | grep -F -- "$KEY" | grep -v grep >/dev/null; then
    fail "encryption key visible in the process list"
fi
pkill -f "X-File-Name: message.txt" || fail "could not find the curl sender"
if wait $RECV; then
    fail "receiver succeeded on an interrupted transfer"
fi
[ -z "$(ls -A "$WORK/out7")" ] || fail "partial file left behind: $(ls -A "$WORK/out7")"
pass "interrupted sender is detected, key not in ps"

echo "All end-to-end tests passed."
