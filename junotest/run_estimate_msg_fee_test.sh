#!/usr/bin/env bash

JUNOTEST_NETWORK=goerli2 JUNOTEST_DB_PATH=../../junotest/juno_goerli2 go test -v -timeout 0 -tags integration > junotest.log 2>&1 &
