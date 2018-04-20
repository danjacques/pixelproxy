#!/bin/bash

set -x

STORAGE_PATH="/mnt/pixelproxy_data/storage"

sudo chown pixelproxy:pixelproxy -R "${STORAGE_PATH}"
