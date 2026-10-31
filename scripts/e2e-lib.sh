# Shared by scripts/e2e-preflight.sh and scripts/serve-test.sh (sourced, not
# run): reserving an e2e run's port, and the server address and database
# derived from it. Each port gets its own server and its own database,
# lewcontext_test_<port>, so runs on different ports never share anything
# but the PostgreSQL container.

# The lock that reserves a port, the same file for every checkout and
# worktree on the machine (not $TMPDIR, which can differ per session).
e2e_lock_file() { echo "/tmp/lewcontext-e2e-$1.lock"; }

e2e_require_flock() {
  if ! command -v flock > /dev/null; then
    echo "flock is missing: it reserves the e2e run's port. It comes with util-linux on Linux; on macOS: brew install flock." >&2
    exit 1
  fi
}

e2e_valid_port() { [[ $1 =~ ^[0-9]+$ ]] && (($1 >= 1024 && $1 <= 65535)); }

e2e_port_in_use() { (exec 3<> "/dev/tcp/127.0.0.1/$1") 2> /dev/null; }

# e2e_reserve PORT takes the port's lock and keeps it open in E2E_LOCK_FD for
# as long as this process (or the command it execs) runs. The lock goes when
# the run ends or crashes. Fails, holding nothing, if another run has it.
e2e_reserve() {
  local lock fd
  lock="$(e2e_lock_file "$1")"
  exec {fd}>> "$lock"
  chmod a+rw "$lock" 2> /dev/null || true
  if flock -n "$fd"; then
    E2E_LOCK_FD=$fd
    return 0
  fi
  exec {fd}>&-
  return 1
}

# e2e_port_free PORT: nobody holds its lock and nothing listens on it.
# Holds nothing afterwards; for suggesting a port.
e2e_port_free() {
  local fd free=1
  exec {fd}>> "$(e2e_lock_file "$1")"
  flock -n "$fd" && ! e2e_port_in_use "$1" && free=0
  exec {fd}>&-
  return $free
}

# e2e_first_free_port: the first free port from 3101 up, or nothing.
e2e_first_free_port() {
  local port
  for port in $(seq 3101 3199); do
    if e2e_port_free "$port"; then
      echo "$port"
      return 0
    fi
  done
  return 1
}

e2e_clash() {
  local port=$1 suggestion
  suggestion="$(e2e_first_free_port || true)"
  {
    echo "Port $port is in use: another e2e run (or a server) has it."
    if [[ -n $suggestion ]]; then
      echo "Run this one beside it on a free port:"
      echo "  make test-e2e E2E_PORT=$suggestion"
    fi
    echo "Or let it pick one: make test-e2e E2E_PORT=auto"
    echo "Each port gets its own server and database: docs/ui/testing.md#running-several-at-once"
  } >&2
  exit 1
}

# e2e_database_url URL PORT: URL with its database name followed by _PORT
# (lewcontext_test → lewcontext_test_3100), keeping the rest.
e2e_database_url() {
  local url=$1 port=$2 base query
  base="${url%%\?*}"
  query="${url#"$base"}"
  echo "${base}_${port}${query}"
}

e2e_database_name() {
  local base="${1%%\?*}"
  echo "${base##*/}"
}

# e2e_database_host URL prints "host port" from a postgresql:// URL.
e2e_database_host() {
  local rest hostport host port=5432
  rest="${1#*://}"
  rest="${rest%%/*}"
  hostport="${rest##*@}"
  if [[ $hostport == \[* ]]; then
    host="${hostport%%]*}"
    host="${host#[}"
    [[ $hostport == *]:* ]] && port="${hostport##*]:}"
  else
    host="${hostport%%:*}"
    [[ $hostport == *:* ]] && port="${hostport##*:}"
  fi
  echo "$host $port"
}

# e2e_require_mailpit: with MAIL_DRIVER=smtp the tests read their links from
# Mailpit, so its SMTP port and its API must both answer.
e2e_require_mailpit() {
  [[ ${MAIL_DRIVER:-} == smtp ]] || return 0
  local api="${MAILPIT_URL#*://}"
  api="${api%%/*}"
  if ! (exec 3<> "/dev/tcp/$SMTP_HOST/$SMTP_PORT") 2> /dev/null || ! (exec 3<> "/dev/tcp/${api%:*}/${api##*:}") 2> /dev/null; then
    echo "Mailpit isn't running at $SMTP_HOST:$SMTP_PORT and $MAILPIT_URL: start it with make db." >&2
    exit 1
  fi
}

e2e_require_database() {
  local host port
  read -r host port <<< "$(e2e_database_host "$DATABASE_URL")"
  if ! (exec 3<> "/dev/tcp/$host/$port") 2> /dev/null; then
    echo "The test database isn't running at $host:$port: start it with make db." >&2
    exit 1
  fi
}
