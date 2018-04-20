#!/bin/bash

set -x
set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Add our local Go to PATH.
export PATH=/opt/golang/go/bin:$PATH

export GOPATH=$DIR/go
export PATH=$GOPATH/bin:$PATH

# Get sudo auth early.
sudo -v

PACKAGE_ROOT=github.com/danjacques/pixelproxy
REPO_ROOT=${GOPATH}/src/${PACKAGE_ROOT}
DEST_USER=pixelproxy
DEST_PATH=/usr/local/bin/pixelproxy

# Update the repository.
git -C "${REPO_ROOT}" clean -f -x -d
git -C "${REPO_ROOT}" fetch origin
git -C "${REPO_ROOT}" checkout origin/master

# Sync/update deps.
go get -u github.com/golang/dep/cmd/dep
(
  cd "${REPO_ROOT}" && \
  dep ensure \
)

# Run the proxy!
go generate "${PACKAGE_ROOT}/..."
go build \
	-o "${DIR}/build/pixelproxy" \
	${PACKAGE_ROOT}/cmd/pixelproxy

sudo mv "${DIR}/build/pixelproxy" "${DEST_PATH}"
sudo chown root:root "${DEST_PATH}"

cat << EOF
The new version of PixelProxy has been deployed. To
start it, run:

sudo systemctl daemon-reload
sudo systemctl restart pixelproxy.service

EOF
