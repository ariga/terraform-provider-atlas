#!/bin/bash

HOSTNAME=registry.terraform.io
NAMESPACE=ariga
TYPE=atlas
VERSION=${2:-0.0.0-pre.0}
TARGET=$(go env GOOS)_$(go env GOARCH)
PACKED="terraform-provider-${TYPE}_${VERSION}_${TARGET}.zip"

PLUGIN_ADDR="${HOSTNAME}/${NAMESPACE}/${TYPE}"
PLUGIN_PATH="${1}/terraform.d/plugins/${PLUGIN_ADDR}"
mkdir -p $PLUGIN_PATH
cp ./dist/${PACKED} $PLUGIN_PATH
