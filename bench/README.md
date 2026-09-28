# Benchmark harness

The benchmark compares the public Go and upstream Rust tokenizer APIs. It first
compares complete binary token dumps, then measures warm encode throughput. Input
loading, vocabulary construction and chunk preparation are outside the timed
region. Rust's `encode_batch` consumes a `Vec<String>`, so its input clone is part
of the batch measurement; Go borrows its input strings.

The pinned Rust harness is in `rust/`. Build it with:

```sh
cargo build --release --locked --manifest-path bench/rust/Cargo.toml
```

Build the Go CLI and run all presets:

```sh
go build -o /tmp/rwkv-tokenizer-go ./cmd/rwkv-tokenizer
python3 bench/compare.py \
  --go /tmp/rwkv-tokenizer-go \
  --rust bench/rust/target/release/rwkv-reference-bench \
  --preset all --corpus /tmp/corpus.txt --iters 100 --runs 5 --threads 10 \
  > bench/readme-results.jsonl
```

`generate.py` creates the mixed fixture and `/tmp/corpus.txt`; set
`CORPUS_GOROOT` when reproducing the exact Go source corpus. The comparison
script records input and vocabulary SHA-256 hashes, compiler versions, host
information, token counts and raw samples. `plot_results.py` renders the README
charts (SVG, standard library only) from the JSONL output:

```sh
python3 bench/plot_results.py bench/readme-results.jsonl bench/throughput.svg
python3 bench/plot_results.py bench/readme-results.jsonl bench/throughput.zh.svg --lang zh
```

The committed JSON results and PNG chart are a dated local measurement on the
Apple M5 test host. They are intended for transparent comparison, not as a
portable performance guarantee across architectures or compiler versions.
