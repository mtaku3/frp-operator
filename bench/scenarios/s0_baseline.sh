#!/usr/bin/env bash
# S0: reference ceiling — always runs against b0. Ignores arg.
exec "$(dirname "$0")/s1_throughput.sh" b0
