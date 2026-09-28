#!/usr/bin/env python3
"""Generate testdata files and the large benchmark corpus.

- testdata/edge_cases.txt      assorted edge-case texts (valid UTF-8)
- testdata/edge_cases_golden.bin  reference token ids from the Rust tokenizer
                                 (uint32 count, then uint32 LE ids)
- testdata/bench_mixed.txt     ~250KB mixed-language benchmark sample
- /tmp/corpus.txt              ~4MB mixed-language benchmark corpus (not committed)

The golden dump is produced with the reference Rust implementation:
the pinned Rust harness in bench/rust (build with cargo build --release
--locked --manifest-path bench/rust/Cargo.toml).

Usage: python3 bench/generate.py <path-to-refbench-binary>
"""
import glob
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
refbench = sys.argv[1] if len(sys.argv) > 1 else None

jp = open(os.path.join(ROOT, "testdata/japanese.txt"), encoding="utf-8").read()
stress = open(os.path.join(ROOT, "testdata/utf8_stress.txt"), encoding="utf-8").read()

chinese = """RWKV 是一个结合了 RNN 与 Transformer 优点的深度学习模型架构。它具有线性复杂度、可并行训练和恒定显存占用的特点，非常适合处理长文本。
分词器（tokenizer）负责将原始文本切分为模型可以理解的词元序列，其速度直接影响整个推理流水线的性能。
本文介绍一个使用 Go 语言实现的高性能 RWKV World 分词器，它基于压缩字典树进行贪心最长匹配，并利用多核处理器并行处理批量文本。
"""

edge_lines = [
    "Today is a beautiful day. 今天是美好的一天。",
    "0", "1", " ", "00", "11", "  ", "000", "111", "   ",
    "0" * 64, "1" * 64, " " * 64, "\t" * 64,
    "1234567890" * 8,
    "3.14159265358979323846264338327950288419716939937510582097494459",
    "don't can't won't it's 'quoted' \"double\" b'notbyte' b'\\x41'",
    "\\n \\t \\x41 \\u0041 literal escapes",
    "😀😃😄😁😆😅😂🤣😊😇🙂🙃😉😌😍🥰😘😗😙😚",
    "👨‍👩‍👧‍👦 flag: 🇺🇸🇯🇵 combining: é é̊",
    "﷐﷑﷒﷓﷔﷕﷖﷗﷘﷙﷚﷛ noncharacters U+FFFE￾ U+FFFF￿",
    "غظxford ةيبرع تاملك Русский язык Ξενοφώντος",
    "กษัตริย์ก่อนหน้าแลถัดไป สององค์ไซร้โง่เขลาเบาปัญญา",
    "我能吞下玻璃而不伤身体。我能吞下玻璃而不傷身體。",
    "ᐊᓕᒍᖅ ᓂᕆᔭᕌᖓᒃᑯ ᓱᕋᙱᑦᑐᓐᓇᖅᑐᖓ",
    "⡍⠜⠇⠑⠹ ⠺⠁⠎ ⠙⠑⠁⠙⠒ ⠞⠕ ⠃⠑⠛⠔ ⠺⠊⠹⠲",
    "ΠΟΛΥΤΡΟΠΟΣ κόσμε ᾿Ελευθεριά",
    "func (t *Tokenizer) Encode(text string) []int32 { return t.AppendEncode(nil, text) }",
    "if err != nil { return nil, fmt.Errorf(\"rwkv: vocab line %d: %w\", lineNo+1, err) }",
    "      indented code block with trailing spaces   ",
    "\r\nline after CRLF\r\nanother CRLF line\r\n",
    "repeat repeat repeat repeat repeat repeat repeat repeat repeat repeat",
    "e" * 100, "n" * 100, " t" * 50,
    "日本語のテキストです。スターシップが打ち上げられた。",
]

edge_text = "\n".join(edge_lines)
with open(os.path.join(ROOT, "testdata/edge_cases.txt"), "w", encoding="utf-8") as f:
    f.write(edge_text)

if refbench:
    subprocess.run(
        [refbench, "dump",
         os.path.join(ROOT, "testdata/edge_cases.txt"),
         os.path.join(ROOT, "assets/rwkv_vocab_v20230424.txt"),
         os.path.join(ROOT, "testdata/edge_cases_golden.bin")],
        check=True,
    )
    print("golden written")
else:
    print("NOTE: no refbench binary given; golden not regenerated")

goroot = os.environ.get("CORPUS_GOROOT") or subprocess.check_output(["go", "env", "GOROOT"], text=True).strip()
go_files = sorted(glob.glob(os.path.join(goroot, "src", "net", "http", "*.go")))
code = "\n".join(open(f, encoding="utf-8", errors="replace").read() for f in go_files)
if not code:
    raise SystemExit(f"No Go source files found in {goroot}; set CORPUS_GOROOT")

mixed = "\n".join(
    [edge_text, code[:120_000]]
    + [chinese] * 20 + [jp] * 3 + [stress] * 2 + [edge_text]
)
with open(os.path.join(ROOT, "testdata/bench_mixed.txt"), "w", encoding="utf-8") as f:
    f.write(mixed)
print("bench_mixed:", len(mixed.encode("utf-8")), "bytes")

parts = [code]
while sum(len(p) for p in parts) < 3_500_000:
    parts.append(code[:1_000_000])
parts += [chinese] * 60 + [jp] * 12 + [stress] * 8 + [edge_text] * 60
corpus = "\n".join(parts)
data = corpus.encode("utf-8")
data.decode("utf-8")  # the Rust harness requires valid UTF-8
with open("/tmp/corpus.txt", "wb") as f:
    f.write(data)
print("corpus:", len(data), "bytes")
