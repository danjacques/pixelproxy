#!/bin/bash

set -x

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

CONFIG_PATH=$1; shift
if [ -z "${CONFIG_PATH}" ]; then
	echo "ERROR: You must specify a configuration YAML path."
	exit 1
fi

# Add our local Go to PATH.
export PATH=/opt/golang/go/bin:$PATH

export GOPATH=$DIR/go
export PATH=$GOPATH/bin:$PATH

PACKAGE_ROOT=github.com/danjacques/pixelproxy
REPO_ROOT=${GOPATH}/src/${PACKAGE_ROOT}

# Update the repository.
git -C "${REPO_ROOT}" pull

# Run the proxy!
go run \
	"${REPO_ROOT}/cmd/test/fakepixelpusher/main.go" \
	-a "192.168.1.211:0" \
	-c "${CONFIG_PATH}"
