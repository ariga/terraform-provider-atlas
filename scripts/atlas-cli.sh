#!/usr/bin/env bash

HOSTNAME=registry.terraform.io
NAMESPACE=ariga
TYPE=atlas
VERSION=${1:-0.0.0-pre.0}
TARGET=$(go env GOOS)_$(go env GOARCH)

PLUGIN_ADDR="${HOSTNAME}/${NAMESPACE}/${TYPE}/${VERSION}/${TARGET}"

./.terraform/providers/$PLUGIN_ADDR/atlas ${@:2}
