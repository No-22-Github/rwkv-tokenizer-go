# Preset measurements (2026-09-28)

Apple M5, Go 1.27.1, Rust 1.98.1; same pinned Rust reference and 4,103,643-byte
corpus as [the previous pass](ROUND2.md). Three rotated-order runs of 50 timed
iterations after three warmup calls, excluding construction and file I/O.
GOMAXPROCS and RAYON_NUM_THREADS are 10; batch requests 64 newline-aligned chunks.
Each preset was measured in a separate block, so machine load and frequency
variation affect comparisons across blocks. These are illustrative local results.

| Preset | Trie MiB | Single MB/s | Batch MB/s |
|---|---:|---:|---:|
| speed | 16.23 | 262.6 | 1495.2 |
| balanced | 3.34 | 244.1 | 1367.1 |
| memory | 2.31 | 175.1 | 1101.3 |

Trie memory includes the struct and backing-array capacities, excluding token
storage, allocator rounding and construction temporaries. These numbers are
reported by `go test -run TestPresets -v`; custom vocabularies use different sizes.

`speed` retains the preceding fast layout. In its interleaved single-thread check,
the pre-presets binary measured 264.1 MB/s and the new binary 262.6 MB/s (0.6%
lower). Batch samples varied substantially, so no gain is attributed to preset
support itself. The purpose of this change is an explicit memory/speed choice.

`balanced` uses direct tables above eight children and scans smaller edge lists.
`memory` allocates no direct tables; it binary-searches large sorted edge lists.
All modes retain inline single-child edges, 16-byte nodes and the two-byte lookup.
Only one layout is built and retained. Preset dispatch occurs once per encode
call; the speed hot loop does not inspect the preset for every input byte.

[Raw samples and hashes](results-2026-09-28-presets.json) include the Rust results.
Rust's public batch API requires cloning the consumed input vector in the timed
loop, as documented in the earlier reports. Batch is an API comparison.

## Reproduction

```sh
go build -o /tmp/rwkv-go ./cmd/rwkv-tokenizer
cargo build --release --locked --manifest-path bench/rust/Cargo.toml
python3 bench/compare.py --go /tmp/rwkv-go \
  --rust bench/rust/target/release/rwkv-reference-bench \
  --preset memory --corpus /tmp/corpus.txt --iters 50 --runs 3 --threads 10
# Repeat with --preset speed and --preset balanced.
go test -run '^$' -bench '^BenchmarkEncodePresets$' -benchmem
```

Generate the large corpus as documented in RESULTS.md. `--baseline` optionally
compares an older Go binary using its default layout (no preset flag is passed
to that binary). The script checks full token dumps against Rust before timing.
All three presets matched the reference on the large corpus.

## Validation

`go test ./...`, `go test -race ./...` and `go vet ./...` passed. Tests cover
all constructors, invalid options, default/override behavior, CLI dumps, custom
vocabulary fallback, an independent greedy oracle, all raw byte pairs, multilingual
fixtures, append operations and parallel batch operations under every preset.
`FuzzPresetParity` passed 5,876,491 executions in a 15-second run. CLI help exposes
`-preset`, and benchmark output identifies the selected preset.
