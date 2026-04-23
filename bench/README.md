# tunnel-bench

Empirical comparison of tunnel backends for the operator's datapath.
Decide between **WireGuard**, **FRP**, and **Chisel** under VPS-like
constraints before committing to one.

## Requirements

- Docker + `docker compose` v2
- Linux host with `tc` available (for WAN emulation — optional)
- ~2 GB RAM free, ~2 CPU cores free

## Layout

```
bench/
├── compose/              # one stack per backend + baseline
│   ├── baseline.yml      # probe → origin direct (ceiling)
│   ├── wireguard.yml
│   ├── frp.yml
│   └── chisel.yml
├── configs/              # backend config files mounted into containers
├── scenarios/            # s0..s10 executable scripts
├── lib/
│   ├── collect.sh        # samples docker stats → CSV
│   └── common.sh         # shared helpers
└── results/              # CSVs + markdown reports (gitignored)
```

## Backends under test

| ID | Backend    | Notes                                         |
|----|------------|-----------------------------------------------|
| wg | WireGuard  | kernel datapath, peers added via `wg set`     |
| fr | FRP        | userspace, hot-reload via `frps` admin API    |
| ch | Chisel     | WebSocket tunnel, SIGHUP to reload auth       |
| b0 | baseline   | no tunnel — probe hits origin directly        |

## Scenarios

See `../docs/` (TBD) or the top-level brainstorm. Short version:

| ID  | What                                 | Gate                              |
|-----|--------------------------------------|-----------------------------------|
| S0  | baseline throughput/latency          | reference only                    |
| S1  | single-stream TCP                    | ≥80% of S0                        |
| S2  | 32-parallel TCP                      | ≥80% of S0                        |
| S3  | wrk HTTP 1KB/100KB                   | p99 overhead ≤5ms                 |
| S4  | UDP throughput + loss                | informational                     |
| S5  | N idle tunnels (1,10,50,100)         | ≤5 MB RSS per tunnel              |
| S6  | N×1Mbps active, accounting check     | ±2% bytes vs iperf                |
| S7  | dynamic add during active flow       | ≤100ms gap, no drops              |
| S8  | dynamic remove                       | clean teardown, no leaks          |
| S9  | reconnect latency                    | <2s                               |
| S10 | 1-hour soak                          | no RSS growth >10%                |

## How to run

```sh
# one backend, one scenario
./scenarios/s1_throughput.sh wg

# full sweep
./scenarios/run_all.sh
```

## Resource caps

`tunnel-server` container is pinned to `--cpus=1 --memory=512m` to mimic a
$5 VPS. Override via `BENCH_CPUS` / `BENCH_MEM` env vars.

## WAN emulation (optional)

Set `BENCH_WAN=1` to apply `tc netem` (50ms RTT, 1% loss, 100Mbps) on the
`wan-net` bridge. Requires host `tc` and `NET_ADMIN`.

## Status

- [x] S0 baseline, S1 single-stream — all three backends
- [x] S2, S3 — wrk/iperf parallel
- [x] S5 idle scaling
- [x] S7 dynamic add (the key one)
- [x] S9 reconnect
