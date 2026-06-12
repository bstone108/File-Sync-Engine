# Metadata DB Candidate Benchmark

## Host facts

- OS/arch: linux/amd64
- Kernel: 6.18.29-Unraid
- CPU: AMD GX-420MC SOC
- RAM: 16328560 kB
- Working directory/storage path: /opt/data/workspace/Projects/file synchronization engine
- Load average: 2.87 4.07 3.73 3/1168 551004
- Go version: go1.19.8

## Results

| Candidate | Imported files | Imported blocks | Lazy hash updates | Content lookups | Status reads | Reopened | Duration |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| pebble | 200 | 800 | 200 | 200 | 80 | true | 10.168408739s |
| badger | 200 | 800 | 200 | 200 | 80 | true | 289.368665ms |
| bbolt | 200 | 800 | 200 | 200 | 80 | true | 25.656524546s |

## Interpretation

These measurements are a same-host comparison only. Do not make the final metadata DB selection from a single host; treat this as low-end stress evidence and repeat finalists on other hardware before selecting the production backend.
