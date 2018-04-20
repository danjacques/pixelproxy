#!/bin/bash

set -x

COMPSIZE="${HOME}/src/compsize/compsize"
STORAGE_ROOT="/mnt/pixelproxy_data/storage/files"

sudo find "${STORAGE_ROOT}" \
	-name '*.protostream' \
	-type f \
	-exec echo "Analyzing {}:" \; \
	-exec "${COMPSIZE}" {} \;
