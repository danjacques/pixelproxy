#!/bin/bash

set -x

journalctl \
	-u pixelproxy.service \
	-o cat \
	--follow --no-tail \
	| awk '{if(NR>1)print}' \
	| jq
