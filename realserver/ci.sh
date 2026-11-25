#!/usr/bin/env bash
# The realserver CI job (tf#86): the stack from run.sh, then the provider's
# real-server tests (internal/realtest) against it. Tests named in
# versions.env's KNOWN_FAILING_TESTS run apart and must fail; see there.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
set -a; . "$here/versions.env"; set +a

"$here/run.sh" up
trap '"$here/run.sh" down' EXIT

export TF_ACC=1 REALSERVER_CREDENTIAL="$here/.data/client.json"
cd "$here/.."

skip=""
if [ -n "${KNOWN_FAILING_TESTS:-}" ]; then
  skip="^(${KNOWN_FAILING_TESTS// /|})\$"
fi
go test -count=1 -timeout 20m -v ${skip:+-skip "$skip"} ./internal/realtest/

for name in ${KNOWN_FAILING_TESTS:-}; do
  echo "realserver: $name is known to fail on guard-control $GUARD_CONTROL_VERSION (versions.env); it must fail here"
  if go test -count=1 -timeout 10m -run "^${name}\$" ./internal/realtest/ >/dev/null 2>&1; then
    echo "realserver: $name now PASSES: remove it from KNOWN_FAILING_TESTS in realserver/versions.env" >&2
    exit 1
  fi
  echo "realserver: $name failed, as expected"
done
