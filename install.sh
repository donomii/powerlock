#!/bin/sh
set -eu

go mod download
go build ./...
