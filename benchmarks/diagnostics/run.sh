#!/bin/sh
set -eu

cd "$(dirname "$0")"
exec go test -run '^$' -tags deadlock -bench=. -benchmem -benchtime=500ms -count=3
