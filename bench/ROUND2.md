# Second optimization pass (2026-09-28)

Same Apple M5, Go 1.27.1, Rust 1.98.1 and pinned Rust reference as
[the first pass](RESULTS.md). The baseline here is the **previously optimized
Go implementation with the two-byte prefix table**, not the initial port.
Default compiler settings; no PGO, assembly, cgo, or new unsafe operations.

Five rotated-order runs with three warmup calls. Large input: 100 iterations
per run; smaller fixtures: 10,000 iterations. GOMAXPROCS and Rayon use 10 workers.
Single-text encoding is serial. Batch measures the same public APIs as before,
including Rust's required Vec<String> input clone. Input setup and vocabulary
loading are outside timing. All outputs are allocated normally.

## Large mixed corpus

4,103,643 bytes, 1,348,484 tokens; same corpus hash as the first pass.

| Mode | Rust MB/s | Previous Go | Current Go | Go gain | Current/Rust |
|---|---:|---:|---:|---:|---:|
| Single | 245.8 | 230.8 | 258.2 | 11.9% | 105.0% |
| Batch, 64 chunks | 1416.7 | 1344.9 | 1590.2 | 18.2% | 112.2% |

These are local warm-throughput measurements on a generated corpus with repeated
source code, not universal Go-versus-Rust ratios or production corpus results.

## Smaller fixtures

| Fixture | Mode | Rust MB/s | Previous Go | Current Go | Go gain | Current/Rust |
|---|---|---:|---:|---:|---:|---:|
| bench_mixed.txt | Single | 388.5 | 327.8 | 575.0 | 75.4% | 148.0% |
| bench_mixed.txt | Batch | 1212.8 | 1044.1 | 1651.5 | 58.2% | 136.2% |
| japanese.txt | Single | 644.0 | 604.0 | 808.3 | 33.8% | 125.5% |
| japanese.txt | Batch | 68.4 | 268.0 | 288.5 | 7.6% | 421.8% |
| utf8_stress.txt | Single | 524.4 | 525.8 | 653.0 | 24.2% | 124.5% |
| utf8_stress.txt | Batch | 585.6 | 807.7 | 991.5 | 22.8% | 169.3% |

The 1,853-byte Japanese fixture remains faster without batching in both
implementations. Small-batch samples show substantial scheduling variability;
their ratios primarily describe API overhead, not per-byte kernel performance.
Raw measurements, ranges and corpus hashes are preserved in
[results-2026-09-28-round2.json](results-2026-09-28-round2.json).

## Implementation

- Give every node with at least two children a direct 256-entry lookup table.
  Leaves share the single-child case with an impossible byte (-1). This removes
  sparse edge scans and one node-type branch from the hot loop.
- Carry the loaded node across iterations instead of reading its ID at the end
  and reloading the same node on the next iteration.
- Remove redundant manual bounds guards, while retaining Go's normal slice and
  array checks.
- Allocate the direct tables exactly once with their final capacity; remove
  the now-unused sparse edge arrays. The previous two-byte shortcut is retained.

Experiments with 12-byte nodes, 8-/12-byte cached prefix entries, and an
intermediate fanout threshold were slower than the selected layout on the large
corpus. They were not retained. No performance claim is made for decode.

## Memory tradeoff

Retained trie storage (struct plus backing-array capacities, excluding token
bytes, vocabulary parsing temporaries and allocator rounding):

| Layout | Nodes | Direct tables | Retained bytes | MiB |
|---|---:|---:|---:|---:|
| Previous | 116,079 | 1,166 | 4,101,758 | 3.91 |
| Current | 116,079 | 14,553 | 17,022,752 | 16.23 |

This adds **12.32 MiB per tokenizer**. It is an explicit memory-for-throughput
tradeoff; reuse a Tokenizer across calls (it is safe for concurrent use).
The counts were obtained from both source snapshots with the embedded vocabulary.
They are not whole-process RSS measurements. No startup-speed claim is made.

## Reproduction

Build the two CLIs as documented in RESULTS.md, then:

```sh
python3 bench/compare.py --go /tmp/rwkv-go \
  --rust bench/rust/target/release/rwkv-reference-bench \
  --corpus /tmp/corpus.txt --iters 100 --runs 5 --threads 10
python3 bench/compare.py --go /tmp/rwkv-go \
  --rust bench/rust/target/release/rwkv-reference-bench \
  --corpus testdata/bench_mixed.txt testdata/japanese.txt testdata/utf8_stress.txt \
  --iters 10000 --runs 5 --threads 10
```

Add `--baseline /path/to/previous-go-binary` for the third column. All source
files started untracked, so the baseline has no commit hash.

## Validation

- Full token dumps for all four measured corpora match both Rust and the previous
  Go binary exactly.
- `go test ./...`, `go test -race ./...` and `go vet ./...` passed.
- Added a deterministic differential test against a vocabulary-scanning greedy
  oracle over 100 random sparse vocabularies, including duplicates and missing
  byte fallbacks. Existing golden, raw-byte, trie and concurrency tests pass.
- A 20-second `FuzzEncodeDecodeBytes` run passed 7,195,825 executions.
- All throughput measurements completed before the final race/fuzz checks and
  memory inspection. Results apply to this host; other architectures are unmeasured.
