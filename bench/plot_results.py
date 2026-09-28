#!/usr/bin/env python3
"""Render the README benchmark chart from compare.py JSONL output."""

import argparse
import json
from pathlib import Path

import matplotlib.pyplot as plt


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("results", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    rows = [json.loads(line) for line in args.results.read_text().splitlines() if line.strip()]
    rows = {row["batch"]: row for row in rows}
    labels = ["Rust", "Go speed", "Go balanced", "Go memory"]
    colors = ["#52606d", "#0f766e", "#2563eb", "#d97706"]
    x = range(len(labels))

    fig, axes = plt.subplots(1, 2, figsize=(10.5, 4.4), dpi=180)
    fig.patch.set_facecolor("white")
    for axis, batch, title in zip(axes, (0, 64), ("Single-text encode", "Batch encode · 64 chunks")):
        row = rows[batch]
        medians = row["median"]
        keys = ["rust", "go-speed", "go-balanced", "go-memory"]
        values = [medians[key] for key in keys]
        bars = axis.bar(x, values, color=colors, width=0.62)
        axis.set_title(title, fontsize=11, pad=10)
        axis.set_ylabel("MB/s")
        axis.set_xticks(list(x), labels, rotation=18, ha="right")
        axis.grid(axis="y", color="#d9e1e8", linewidth=0.8)
        axis.set_axisbelow(True)
        axis.spines[["top", "right"]].set_visible(False)
        for bar, value in zip(bars, values):
            axis.text(bar.get_x() + bar.get_width() / 2, value + max(values) * 0.018,
                      f"{value:.0f}", ha="center", va="bottom", fontsize=8)

    fig.suptitle("RWKV World tokenizer throughput", fontsize=14, fontweight="bold")
    fig.text(0.5, 0.01, "Apple M5 · Go 1.27.1 · Rust 1.98.1 · 4.1 MB mixed corpus · median of 5 runs",
             ha="center", fontsize=8, color="#52606d")
    fig.tight_layout(rect=(0, 0.06, 1, 0.92))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(args.output, bbox_inches="tight")


if __name__ == "__main__":
    main()
