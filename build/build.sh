#!/usr/bin/env bash

VERSION=1.0

docker build --platform=linux/amd64 --no-cache --provenance=false -t task-definitions-cleanup:$VERSION -f build/Dockerfile .
