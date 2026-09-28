# First-pass comparison (2026-09-28)

Historical results for the first optimization. See [the second pass](ROUND2.md)
for the current implementation and its memory tradeoff.

Apple M5 (10 cores), macOS arm64; Go 1.27.1, Rust 1.98.1.
Reference: https://github.com/cahya-wirawan/rwkv-tokenizer at
`63b21e891d0c8efd95997d797049bb7f92098d8c`, with Cargo.lock checked in.
Default Go build and Cargo release profile, no native-CPU flags or PGO.
Both use this repository's vocabulary. Decimal MB/s; higher is better.

Five runs per case, rotated execution order, three warmup calls before timing.
Loading vocabulary, reading files, and preparing chunks are outside timing.
Each encode allocates its output; Go returns int32 IDs, Rust returns u16 IDs.
Rust's public batch API consumes Vec<String>, so its required input clone is
included in batch timing; Go borrows its input slice. This is an end-to-end
public API comparison, not an allocation-normalized kernel comparison.
GOMAXPROCS and RAYON_NUM_THREADS are both 10; single-text encoding is serial.
Batch requests 64 chunks, using identical newline-based splitting. Tiny inputs
may produce fewer chunks. Newline boundaries can change tokenization relative
to the unsplit input, so batch and single-text token counts need not agree.

## Large corpus

4,103,643 bytes, 1,348,484 tokens; 50 iterations per run.
SHA256: `aae3baa3325f10f0ff5c51749485d538e456715c2c5b920442837bccbbc0745b`.
This generated corpus contains repeated Go source and multilingual fixtures;
it is not a representative production dataset or the upstream Wikipedia benchmark.

| Mode | Rust MB/s | Go before | Go after | Before/Rust | After/Rust | Go gain |
|---|---:|---:|---:|---:|---:|---:|
| Single | 246.6 | 206.0 | 231.1 | 83.5% | 93.7% | 12.2% |
| Batch | 1462.9 | 1220.8 | 1361.2 | 83.5% | 93.0% | 11.5% |

## Smaller fixtures

5,000 iterations per run; medians of five runs. Tiny-input results still
include scheduling and allocation overhead and should not be generalized.

| Fixture | Mode | Rust MB/s | Go before | Go after | After/Rust | Go gain |
|---|---|---:|---:|---:|---:|---:|
| bench_mixed.txt | Single | 385.9 | 310.3 | 323.8 | 83.9% | 4.4% |
| bench_mixed.txt | Batch | 1241.3 | 1038.7 | 1086.3 | 87.5% | 4.6% |
| japanese.txt | Single | 680.2 | 512.8 | 643.2 | 94.6% | 25.4% |
| japanese.txt | Batch | 75.5 | 294.1 | 314.8 | 417.0% | 7.0% |
| utf8_stress.txt | Single | 519.7 | 439.9 | 518.2 | 99.7% | 17.8% |
| utf8_stress.txt | Batch | 771.4 | 864.2 | 937.1 | 121.5% | 8.4% |

The Japanese fixture is only 1,853 bytes: batching is slower than serial in
both implementations. Its Go/Rust batch ratio mainly reflects overhead.
Raw samples and input hashes: [results-2026-09-28.json](results-2026-09-28.json).
One small-mixed Rust sample was a low outlier (208 MB/s versus 384–390 MB/s);
all samples are retained and the reported statistic is the median.

## Reproduction

```sh
go build -o /tmp/rwkv-go ./cmd/rwkv-tokenizer
cargo build --release --locked --manifest-path bench/rust/Cargo.toml
python3 bench/compare.py --go /tmp/rwkv-go \
  --rust bench/rust/target/release/rwkv-reference-bench \
  --corpus testdata/bench_mixed.txt testdata/japanese.txt testdata/utf8_stress.txt \
  --iters 5000 --runs 5 --threads 10
```

For the large generated corpus, run `python3 bench/generate.py` and pass
`--corpus /tmp/corpus.txt --iters 50`. The generator also rewrites the mixed
fixture; Go source selection depends on GOROOT, so compare its SHA256 to the
recorded sample before treating it as identical. Set CORPUS_GOROOT to select a
specific Go installation. `--baseline /path/to/old/go/binary` adds a third build.
The before measurements used the unmodified workspace built before editing;
the workspace started entirely untracked, so there is no baseline commit hash.
The comparison script verifies complete single-text token dumps before timing.

## Change and validation

A 65,536-entry int32 table bypasses the first two trie edges. It retains the
one-byte terminal fallback for nonterminal pairs and stops on absent prefixes.
Cost: 256 KiB per tokenizer plus initialization; no API change or new dependency
in the Go library. Missing root entries are explicitly initialized to -1.

`go test ./...`, `go test -race ./...`, `go vet ./...` passed. A 10-second
FuzzEncodeDecodeBytes run passed about 4.04 million executions. Added sparse
custom-vocabulary tests for pair misses, prefix fallback, and truncated inputs.
Full single-text token dumps match Rust for all four measured corpora.
No cross-platform performance claim, decode speedup, or startup improvement
is inferred from these encode measurements.
