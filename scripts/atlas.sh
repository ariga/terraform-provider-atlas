#!/usr/bin/env bash
curl -sSf https://atlasgo.sh | \
  sh -s -- -y --no-install --platform $1 --output $2
