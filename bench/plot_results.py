#!/usr/bin/env python3
"""Render the README benchmark chart (SVG) from compare.py JSONL output.

Uses only the standard library. The SVG follows the viewer's light/dark
color scheme and is rendered in English or Simplified Chinese:

    python3 bench/plot_results.py bench/readme-results.jsonl bench/throughput.svg
    python3 bench/plot_results.py bench/readme-results.jsonl bench/throughput.zh.svg --lang zh
"""

import argparse
import json
from html import escape
from pathlib import Path

# Retained trie storage per preset with the embedded vocabulary, in MiB,
# as reported by `go test -run TestPresets -v`.
TRIE_MIB = {"go-speed": 16.23, "go-balanced": 3.34, "go-memory": 2.31}

KEYS = ["rust", "go-speed", "go-balanced", "go-memory"]
COLOR_VAR = {"rust": "--rust", "go-speed": "--s1", "go-balanced": "--s2", "go-memory": "--s3"}

TEXT = {
    "en": {
        "title": "RWKV World tokenizer: throughput and memory",
        "labels": {"rust": "Rust (original)", "go-speed": "Go speed",
                   "go-balanced": "Go balanced", "go-memory": "Go memory"},
        "panels": ["Single-text encode · MB/s", "Batch encode, 64 chunks · MB/s", "Trie memory · MiB"],
        "vs": "vs Rust",
        "note": "Apple M5 · Go 1.27.1 · Rust 1.98.1 · 4.1 MB mixed corpus · median of 5 runs · higher throughput / lower memory is better",
        "font": "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif",
    },
    "zh": {
        "title": "RWKV World 分词器：吞吐量与内存",
        "labels": {"rust": "Rust（原作）", "go-speed": "Go speed",
                   "go-balanced": "Go balanced", "go-memory": "Go memory"},
        "panels": ["单文本编码 · MB/s", "批量编码（64 块）· MB/s", "Trie 内存 · MiB"],
        "vs": "相对 Rust",
        "note": "Apple M5 · Go 1.27.1 · Rust 1.98.1 · 4.1 MB 混合语料 · 5 轮中位数 · 吞吐越高越好，内存越低越好",
        "font": "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', 'Noto Sans CJK SC', sans-serif",
    },
}

STYLE = """
svg { --bg:#fcfcfb; --ink:#0b0b0b; --ink2:#52514e; --muted:#8a8984; --grid:#e6e5e0;
      --rust:#8a8984; --s1:#2a78d6; --s2:#eb6834; --s3:#1baf7a; }
@media (prefers-color-scheme: dark) {
  svg { --bg:#1a1a19; --ink:#ffffff; --ink2:#c3c2b7; --muted:#8f8e86; --grid:#33332f;
        --rust:#8f8e86; --s1:#3987e5; --s2:#d95926; --s3:#199e70; }
}
.bg { fill: var(--bg); }
.title { fill: var(--ink); font-size: 17px; font-weight: 650; }
.panel { fill: var(--ink); font-size: 13px; font-weight: 600; }
.label { fill: var(--ink2); font-size: 12px; }
.value { fill: var(--ink); font-size: 12px; font-weight: 600; }
.ratio { fill: var(--muted); font-size: 11px; }
.note { fill: var(--muted); font-size: 11px; }
.grid { stroke: var(--grid); stroke-width: 1; }
"""


def bar_path(x, y, w, h, r=4):
    """Horizontal bar anchored at x with a rounded data end."""
    r = min(r, w, h / 2)
    return (f"M{x:.1f},{y:.1f}h{w - r:.1f}a{r},{r} 0 0 1 {r},{r}v{h - 2 * r:.1f}"
            f"a{r},{r} 0 0 1 {-r},{r}h{-(w - r):.1f}z")


def render(rows, lang):
    t = TEXT[lang]
    single, batch = rows[0]["median"], rows[64]["median"]
    panels = [
        ({k: single[k] for k in KEYS}, "{:.0f}", True),
        ({k: batch[k] for k in KEYS}, "{:.0f}", True),
        (TRIE_MIB, "{:.2f}", False),
    ]

    width, label_w, panel_w, gap = 920, 118, 238, 26
    top, row_h, bar_h = 92, 42, 20
    height = top + row_h * len(KEYS) + 58
    out = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" '
           f'width="{width}" height="{height}" font-family="{escape(t["font"], quote=True)}" '
           f'role="img" aria-label="{escape(t["title"])}">',
           f"<style>{STYLE}</style>",
           f'<rect class="bg" width="{width}" height="{height}" rx="12"/>',
           f'<text class="title" x="24" y="38">{escape(t["title"])}</text>']

    for i, key in enumerate(KEYS):
        y = top + i * row_h + bar_h / 2 + 4
        out.append(f'<text class="label" x="24" y="{y:.1f}">{escape(t["labels"][key])}</text>')

    x0 = 24 + label_w
    for p, (values, fmt, vs_rust) in enumerate(panels):
        px = x0 + p * (panel_w + gap)
        scale = (panel_w - 84) / max(values.values())
        out.append(f'<text class="panel" x="{px}" y="{top - 18}">{escape(t["panels"][p])}</text>')
        out.append(f'<line class="grid" x1="{px}" y1="{top - 8}" x2="{px}" '
                   f'y2="{top + row_h * len(KEYS) - 10}"/>')
        for i, key in enumerate(KEYS):
            y = top + i * row_h
            if key not in values:
                out.append(f'<text class="ratio" x="{px + 8}" y="{y + bar_h / 2 + 4:.1f}">—</text>')
                continue
            v = values[key]
            w = max(v * scale, 2)
            label = fmt.format(v)
            out.append(f'<path d="{bar_path(px + 1, y, w, bar_h)}" style="fill:var({COLOR_VAR[key]})">'
                       f'<title>{escape(t["labels"][key])}: {label}</title></path>')
            out.append(f'<text class="value" x="{px + w + 8:.1f}" y="{y + bar_h / 2 + 4:.1f}">{label}</text>')
            if vs_rust and key != "rust":
                ratio = v / values["rust"]
                out.append(f'<text class="ratio" x="{px + w + 8:.1f}" y="{y + bar_h + 9:.1f}">'
                           f'{ratio:.2f}× {escape(t["vs"])}</text>')

    out.append(f'<text class="note" x="24" y="{height - 20}">{escape(t["note"])}</text>')
    out.append("</svg>")
    return "\n".join(out) + "\n"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("results", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--lang", choices=sorted(TEXT), default="en")
    args = parser.parse_args()
    rows = [json.loads(line) for line in args.results.read_text().splitlines() if line.strip()]
    rows = {row["batch"]: row for row in rows}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(render(rows, args.lang), encoding="utf-8")


if __name__ == "__main__":
    main()
