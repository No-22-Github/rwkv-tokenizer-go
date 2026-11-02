# rwkv-tokenizer-go

[![Go Reference](https://pkg.go.dev/badge/github.com/No-22-Github/rwkv-tokenizer-go.svg)](https://pkg.go.dev/github.com/No-22-Github/rwkv-tokenizer-go)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) · **简体中文**

一个高性能、纯 Go 实现的 **RWKV World 分词器**（适用于 RWKV v5/v6 及之后的模型）。
输出的 token id 与 Rust 原作逐字节一致，内置标准词表，零依赖。

```go
tok, _ := rwkvtok.New() // 标准词表已内嵌，无需额外文件
ids := tok.Encode("Today is a beautiful day. 今天是美好的一天。")
// [33520 4600 332 59219 21509 47 33 10381 11639 13091 15597 11685 14734 10250 11639 10080]
text, _ := tok.Decode(ids)
```

## 特性

- **结果精确**：与 Rust 原作输出完全一致，已在 4.1 MB 多语言语料、约 40 个边界用例
  以及原作自带的测试向量上验证。
- **速度快**：默认预设下，单线程编码是 Rust 原作的 1.04 倍，并行批量编码是 1.15 倍（Apple M5）。
- **零依赖**：纯 Go；65,529 个 token 的标准词表 `rwkv_vocab_v20230424.txt` 通过
  `go:embed` 内置，同时支持加载自定义词表。
- **内存可调**：三种预设在 trie 内存（16.2 → 2.3 MiB）和速度之间取舍，输出完全相同。
- **任意字节无损**：每个字节都有兜底 token，原始字节甚至非法 UTF-8 都能通过
  `EncodeBytes` / `DecodeBytes` 完整往返。
- **并发安全**：`Tokenizer` 构造后只读，可以在多个 goroutine 间共享同一个实例。

<p align="center">
  <img src="bench/throughput.zh.svg" alt="编码吞吐量与 trie 内存：Rust 原作与三种 Go 预设对比" width="920">
</p>

## 在 Go 项目中使用

```sh
go get github.com/No-22-Github/rwkv-tokenizer-go
```

需要 Go 1.24 及以上。包名为 `rwkvtok`：

```go
import rwkvtok "github.com/No-22-Github/rwkv-tokenizer-go"
```

基本用法：

```go
tok, err := rwkvtok.New()
if err != nil {
	log.Fatal(err)
}

ids := tok.Encode("Hello, RWKV!")        // 文本 -> []int32
text, err := tok.Decode(ids)             // []int32 -> 文本（会校验 UTF-8）

buf = tok.AppendEncode(buf[:0], line)    // 热循环中复用 []int32 缓冲区
all := tok.EncodeBatch(docs)             // 多核并行编码一批文本

raw := tok.EncodeBytes([]byte{0xff, 0xfe}) // 任意字节，不要求是 UTF-8
b, _ := tok.DecodeBytes(raw)               // -> []byte{0xff, 0xfe}
```

加载同[格式](#词表格式)的自定义词表：

```go
tok, err := rwkvtok.NewFromFile("my_vocab.txt")
```

## 预设

在构造时一次性选定内存与速度的取舍。所有预设输出相同的 id，也都支持自定义词表。

```go
tok, _ := rwkvtok.New()                                        // PresetSpeed（默认）
tok, _ := rwkvtok.New(rwkvtok.WithPreset(rwkvtok.PresetBalanced))
tok, _ := rwkvtok.NewFromFile("vocab.txt", rwkvtok.WithPreset(rwkvtok.PresetMemory))
```

| 预设 | CLI 参数 | Trie 内存 | 单线程 / 批量编码 | 结构 |
|---|---|---:|---:|---|
| `PresetSpeed`（默认） | `speed` | 16.23 MiB | 258 / 1660 MB/s | 每个分叉节点都用 256 项直接查找表 |
| `PresetBalanced` | `balanced` | 3.34 MiB | 245 / 1474 MB/s | 仅大分叉用直接表，其余用短边表 |
| `PresetMemory` | `memory` | 2.31 MiB | 181 / 1080 MB/s | 有序边表，大分叉用二分查找 |

**怎么选：** 服务端和批处理保持默认即可；同一进程里要跑很多个分词器时用 `balanced`，
内存约小 5 倍、速度保留约 95%；内存很紧张的环境用 `memory`。

内存指标准词表下常驻的 trie 存储（不含 token 字节和分配器开销）。传入未知预设会返回错误。
详见 [bench/PRESETS.md](bench/PRESETS.md)。

## 命令行工具

```sh
go install github.com/No-22-Github/rwkv-tokenizer-go/cmd/rwkv-tokenizer@latest
```

```sh
echo "你好，世界" | rwkv-tokenizer encode                  # 文本 -> id
rwkv-tokenizer decode <<< "10381 11639 13091"             # id -> 文本
rwkv-tokenizer encode -preset memory -in corpus.txt       # 指定预设
rwkv-tokenizer count -in corpus.txt                       # 统计 token 数
rwkv-tokenizer count -jsonl -in rows.jsonl                # 给每行 JSON 的字符串字段加 <field>_tok
rwkv-tokenizer bench -in corpus.txt -iters 20             # 单线程吞吐
rwkv-tokenizer bench -in corpus.txt -iters 20 -batch 64   # 并行吞吐
rwkv-tokenizer dump -in corpus.txt -out ids.bin           # 二进制：u32 数量 + u32 小端 id
```

所有子命令都支持 `-vocab FILE` 指定自定义词表，以及 `-preset`。

## 性能测试

Apple M5，Go 1.27.1，Rust 1.98.1。语料为 4.1 MB 混合文本（以 Go 源码为主，另含中文、
日文和 UTF-8 压力文本；共 1,348,484 个 token），预热后交替运行 5 轮、每轮 100 次，取中位数。
批量模式把输入按换行切成 64 块，使用 10 个 worker。计时前会先比对完整的 token 输出与 Rust 一致。

| 模式 | Rust 原作 | Go `speed` | Go `balanced` | Go `memory` |
|---|---:|---:|---:|---:|
| 单线程编码 | 249.2 MB/s | **258.2 MB/s**（1.04×） | 245.0 MB/s（0.98×） | 181.3 MB/s（0.73×） |
| 批量编码，64 块 | 1449.7 MB/s | **1660.2 MB/s**（1.15×） | 1473.6 MB/s（1.02×） | 1080.3 MB/s（0.75×） |

以上是单台机器上的热吞吐数据，不代表普遍意义上的 Go 与 Rust 性能比。Rust 的批量接口会消耗输入，
因此它的计时中包含一次输入克隆。小语料结果、内存细节和复现步骤见 [bench/](bench/README.md)
（[ROUND2.md](bench/ROUND2.md)、[PRESETS.md](bench/PRESETS.md)）。

## 为什么快

- 整个 trie 平铺在单个数组里，使用 `int32` 下标和 16 字节节点，没有需要 GC 扫描的指针。
- 分叉节点使用 256 项直接子节点表（`speed` 预设），所有预设都带有双字节前缀快捷表。
- 热循环几乎不分配内存，并且在迭代之间直接沿用当前节点，不重复加载。
- `EncodeBatch` 按 `GOMAXPROCS` 把任务分发到多个 goroutine。

## 正确性

- `TestEncodeBeautifulDay` 校验 Rust 原作测试集中的精确 id。
- `TestGoldenEdgeCases` 与 Rust 分词器导出的 id 对比约 40 个刁钻输入：连续空白/数字、引号、
  emoji ZWJ 序列、非字符码点、CJK、框线字符、CRLF 等（见 `bench/generate.py`）。
- 完整的 4.1 MB 语料在每种预设下都与 Rust 输出逐字节一致。
- 另有：基于随机词表、与暴力贪心实现对照的差分测试；随机字节（含非法 UTF-8）的性质测试；
  以及 `FuzzEncodeDecodeBytes` / `FuzzPresetParity` 两个模糊测试。

```sh
go test ./...
go test -race ./...
```

## API 参考

所有导出函数和方法的速查。带注释的完整文档见
[pkg.go.dev](https://pkg.go.dev/github.com/No-22-Github/rwkv-tokenizer-go)。

```go
func New(opts ...Option) (*Tokenizer, error)                     // 内嵌标准词表
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
func (t *Tokenizer) EncodeBatch(texts []string) [][]int32       // 并行
func (t *Tokenizer) EncodeBatchBytes(datas [][]byte) [][]int32  // 并行

func (t *Tokenizer) Decode(ids []int32) (string, error)         // 校验 UTF-8
func (t *Tokenizer) DecodeBytes(ids []int32) ([]byte, error)    // 不校验 UTF-8
func (t *Tokenizer) VocabSize() int
func (t *Tokenizer) Preset() Preset
```

行为约定（与 Rust 原作一致）：

- World 词表的 id 范围是 1..65529，可以放进 `uint16`；id 0 解码为一个 NUL 字节。
- `Decode` 遇到词表外的 id 或结果不是合法 UTF-8 时返回错误；`DecodeBytes` 跳过 UTF-8 校验。
- 编码方式是在字节 trie 上做贪心最长前缀匹配，没有 BPE 合并，也没有预分词。

## 词表格式

每行一个 token：`<id> <Python repr 形式的 token> <字节长度>`，例如

```
34 '!' 1
10381 '今' 3
129 b'\x80' 1
```

字符串 token 使用 Python repr 转义（`\xHH`、`\uHHHH`、`\UHHHHHHHH`、`\n`、`\t`、`\r`、
`\\`、`\'`、`\"`）；`b'…'` 形式的 token 是以 `\xHH` 表示的原始字节。

## 致谢

- 算法与参考行为来自 Rust 原作
  [cahya-wirawan/rwkv-tokenizer](https://github.com/cahya-wirawan/rwkv-tokenizer)（Apache-2.0）。
- 词表 `rwkv_vocab_v20230424.txt` 来自 [BlinkDL/RWKV-LM](https://github.com/BlinkDL/RWKV-LM)（Apache-2.0）。

## 许可证

[Apache License 2.0](LICENSE)
