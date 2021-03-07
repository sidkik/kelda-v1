#!/bin/bash
set -e

LICENSE_OVERRIDE="./scripts/internal-usage-license" "$(dirname "$0")/setup_cluster.sh"
go test -timeout 1h -count 1 -v -tags ci ./ci
