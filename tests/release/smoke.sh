#!/bin/sh
set -eu

mkdir -p build

go build -o build/apix-engine ./cmd/apix-engine/

go build -o build/apix-cli ./cmd/apix-cli/

if [ ! -x build/apix-engine ] || [ ! -x build/apix-cli ]; then
  echo "build failed"
  exit 1
fi

# Verify CLI help runs
build/apix-cli --help >/dev/null 2>&1 || { echo "apix-cli help failed"; exit 1; }

echo "smoke ok"
