# rwkv-tokenizer-go

[![Go Reference](https://pkg.go.dev/badge/github.com/No-22-Github/rwkv-tokenizer-go.svg)](https://pkg.go.dev/github.com/No-22-Github/rwkv-tokenizer-go)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**English** · [简体中文](README.zh-CN.md)

A fast, pure-Go implementation of the **RWKV World tokenizer** (RWKV v5/v6 and
later). It produces token ids that are byte-for-byte identical to the original
Rust implementation, embeds the standard vocabulary, and has zero dependencies.

```go
tok, _ := rwkvtok.New() // standard vocab is embedded, no files needed
ids := tok.Encode("Today is a beautiful day. 今天是美好的一天。")
// [33520 4600 332 59219 21509 47 33 10381 11639 13091 15597 11685 14734 10250 11639 10080]
text, _ := tok.Decode(ids)
```

## Highlights

- **Exact** — same ids as the Rust original, verified on a 4.1 MB
  multilingual corpus, ~40 edge cases and its own test vectors.
- **Fast** — 1.04× the Rust original single-threaded and 1.15× with parallel
  batch encoding (Apple M5, default preset).
- **Zero dependencies** — pure Go; the 65,529-token vocab
  `rwkv_vocab_v20230424.txt` is built in via `go:embed`. Custom vocab files are
  supported too.
- **Tunable memory** — three presets trade trie memory (16.2 → 2.3 MiB) for
  speed, with identical output.
- **Lossless on any bytes** — every byte has a fallback token, so raw and even
  invalid UTF-8 input round-trips through `EncodeBytes` / `DecodeBytes`.
- **Concurrency-safe** — a `Tokenizer` is immutable after construction; share
  one across goroutines.

<p align="center">
  <img src="bench/throughput.svg" alt="Encode throughput and trie memory: Rust original vs the three Go presets" width="920">
</p>

## Using in a Go project

```sh
go get github.com/No-22-Github/rwkv-tokenizer-go
```

Requires Go 1.24 or later. The package name is `rwkvtok`:

```go
import rwkvtok "github.com/No-22-Github/rwkv-tokenizer-go"
```

Basic usage:

```go
tok, err := rwkvtok.New()
if err != nil {
	log.Fatal(err)
}

ids := tok.Encode("Hello, RWKV!")        // text -> []int32
text, err := tok.Decode(ids)             // []int32 -> text (validates UTF-8)

buf = tok.AppendEncode(buf[:0], line)    // reuse a []int32 buffer in hot loops
all := tok.EncodeBatch(docs)             // encode many texts on all cores

raw := tok.EncodeBytes([]byte{0xff, 0xfe}) // arbitrary bytes, UTF-8 not required
b, _ := tok.DecodeBytes(raw)               // -> []byte{0xff, 0xfe}
```

Load a custom vocabulary in the same [format](#vocab-format):

```go
tok, err := rwkvtok.NewFromFile("my_vocab.txt")
```

## Presets

Pick the memory/speed tradeoff once, at construction time. All presets produce
the same ids and work with custom vocabularies.

```go
tok, _ := rwkvtok.New()                                        // PresetSpeed (default)
tok, _ := rwkvtok.New(rwkvtok.WithPreset(rwkvtok.PresetBalanced))
tok, _ := rwkvtok.NewFromFile("vocab.txt", rwkvtok.WithPreset(rwkvtok.PresetMemory))
```

| Preset | CLI | Trie memory | Single / batch encode | Layout |
|---|---|---:|---:|---|
| `PresetSpeed` *(default)* | `speed` | 16.23 MiB | 258 / 1660 MB/s | Direct 256-entry table on every branching node |
| `PresetBalanced` | `balanced` | 3.34 MiB | 245 / 1474 MB/s | Direct tables only for large branches, short edge lists elsewhere |
| `PresetMemory` | `memory` | 2.31 MiB | 181 / 1080 MB/s | Sorted edge lists, binary search on large ones |

**Rule of thumb:** keep the default for servers and batch jobs; use `balanced`
when running many tokenizers in one process — it's ~5× smaller at ~95% of the
speed; use `memory` for tight environments.

Memory is the retained trie storage for the standard vocab (excluding token
bytes and allocator overhead). An unknown preset returns an error. More in
[bench/PRESETS.md](bench/PRESETS.md).

## CLI

```sh
go install github.com/No-22-Github/rwkv-tokenizer-go/cmd/rwkv-tokenizer@latest
```

```sh
echo "你好，世界" | rwkv-tokenizer encode                  # text -> ids
rwkv-tokenizer decode <<< "10381 11639 13091"             # ids -> text
rwkv-tokenizer encode -preset memory -in corpus.txt       # choose a preset
rwkv-tokenizer count -in corpus.txt                       # count tokens
rwkv-tokenizer count -jsonl -in rows.jsonl                # add <field>_tok to each JSON line
rwkv-tokenizer bench -in corpus.txt -iters 20             # single-thread throughput
rwkv-tokenizer bench -in corpus.txt -iters 20 -batch 64   # parallel throughput
rwkv-tokenizer dump -in corpus.txt -out ids.bin           # binary: u32 count + u32 LE ids
```

All commands accept `-vocab FILE` for a custom vocabulary and `-preset`.

## Benchmarks

Apple M5, Go 1.27.1, Rust 1.98.1. A 4.1 MB mixed corpus (mostly Go source
code plus Chinese, Japanese and UTF-8 stress text; 1,348,484 tokens), median of five interleaved 100-iteration runs after
warmup. Batch mode splits the input into 64 newline-aligned chunks on 10
workers. Full token dumps are checked against Rust before any timing.

| Mode | Rust original | Go `speed` | Go `balanced` | Go `memory` |
|---|---:|---:|---:|---:|
| Single-thread encode | 249.2 MB/s | **258.2 MB/s** (1.04×) | 245.0 MB/s (0.98×) | 181.3 MB/s (0.73×) |
| Batch encode, 64 chunks | 1449.7 MB/s | **1660.2 MB/s** (1.15×) | 1473.6 MB/s (1.02×) | 1080.3 MB/s (0.75×) |

These are warm-throughput numbers from one machine, not a universal Go-vs-Rust
ratio. Rust's batch API consumes its input, so its timed loop includes an input
clone. Smaller fixtures, memory details and reproduction steps are in
[bench/](bench/README.md) ([ROUND2.md](bench/ROUND2.md),
[PRESETS.md](bench/PRESETS.md)).

## How it's fast

- A flat trie in a single array with `int32` indices and 16-byte nodes — no
  pointers for the GC to scan.
- Direct 256-entry child tables on branching nodes (`speed`) and a two-byte
  prefix shortcut table in every preset.
- An allocation-light hot loop that carries the current node across
  iterations instead of reloading it.
- `EncodeBatch` fans work out across `GOMAXPROCS` goroutines.

## Correctness

- `TestEncodeBeautifulDay` checks the exact ids from the Rust original's test suite.
- `TestGoldenEdgeCases` compares against ids dumped from the Rust tokenizer for
  ~40 tricky inputs: whitespace/digit runs, quotes, emoji ZWJ sequences,
  noncharacters, CJK, box drawing, CRLF, … (see `bench/generate.py`).
- The full 4.1 MB corpus matches the Rust dump byte-for-byte under every preset.
- A differential test against a brute-force greedy oracle over random
  vocabularies, property tests on random (including invalid UTF-8) bytes, and
  fuzz targets `FuzzEncodeDecodeBytes` / `FuzzPresetParity`.

```sh
go test ./...
go test -race ./...
```

## API reference

Every exported function and method at a glance. Full docs with comments are on
[pkg.go.dev](https://pkg.go.dev/github.com/No-22-Github/rwkv-tokenizer-go).

```go
func New(opts ...Option) (*Tokenizer, error)                     // embedded standard vocab
func NewFromFile(path string, opts ...Option) (*Tokenizer, error)
func NewFromReader(r io.Reader, opts ...Option) (*Tokenizer, error)
func NewFromBytes(data []byte, opts ...Option) (*Tokenizer, error)
func WithPreset(preset Preset) Option
func WithEOD() Option                                           // "<EOD>" as token 0, like rwkv_lightning
func DefaultVocab() ([]byte, error)
func (t *Tokenizer) Count(text string) int
func (t *Tokenizer) CountBytes(data []byte) int
func (t *Tokenizer) CountJSONL(dst io.Writer, src io.Reader) error
func (t *Tokenizer) VocabSHA256() string

func (t *Tokenizer) Encode(text string) []int32
func (t *Tokenizer) AppendEncode(dst []int32, text string) []int32
func (t *Tokenizer) EncodeBytes(data []byte) []int32
func (t *Tokenizer) AppendEncodeBytes(dst []int32, data []byte) []int32
func (t *Tokenizer) EncodeBatch(texts []string) [][]int32       // parallel
func (t *Tokenizer) EncodeBatchBytes(datas [][]byte) [][]int32  // parallel

func (t *Tokenizer) Decode(ids []int32) (string, error)         // validates UTF-8
func (t *Tokenizer) DecodeBytes(ids []int32) ([]byte, error)    // no UTF-8 check
func (t *Tokenizer) VocabSize() int
func (t *Tokenizer) Preset() Preset
```

Behavior (same as the Rust original):

- World vocab ids are 1..65529 and fit in `uint16`; id 0 decodes to a NUL byte.
- `Decode` fails on ids outside the vocab or on output that is not valid UTF-8;
  `DecodeBytes` skips the UTF-8 check.
- Encoding is greedy longest-prefix match over a byte trie — no BPE merges, no
  pre-tokenization.

## Vocab format

One token per line: `<id> <python-repr token> <byte length>`, for example

```
34 '!' 1
10381 '今' 3
129 b'\x80' 1
```

String tokens use Python repr escapes (`\xHH`, `\uHHHH`, `\UHHHHHHHH`, `\n`,
`\t`, `\r`, `\\`, `\'`, `\"`); `b'…'` tokens are raw bytes written as `\xHH`.

## Acknowledgements

- Algorithm and reference behavior from the Rust original,
  [cahya-wirawan/rwkv-tokenizer](https://github.com/cahya-wirawan/rwkv-tokenizer) (Apache-2.0).
- Vocabulary `rwkv_vocab_v20230424.txt` from
  [BlinkDL/RWKV-LM](https://github.com/BlinkDL/RWKV-LM) (Apache-2.0).

## License

[Apache License 2.0](LICENSE)
