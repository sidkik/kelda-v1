#!/bin/bash
set -e

default_license_path="$(dirname "$0")/kelda-license"
license_path="${LICENSE_OVERRIDE:-${default_license_path}}"

# Deploy the newest version of the Kelda minion.
kelda setup-minion --force --license "${license_path}"
