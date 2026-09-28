#!/usr/bin/env python3
"""Check token parity and compare Go presets against the pinned Rust CLI.

Emits one JSON object per corpus/mode. Use --preset all for an interleaved
comparison with a common Rust baseline. See bench/README.md for methodology.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess
import tempfile


def positive_int(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--baseline", help="older Go binary, using its default preset")
    parser.add_argument("--preset", choices=["speed", "balanced", "memory", "all"], default="speed")
    parser.add_argument("--rust", required=True)
    parser.add_argument("--corpus", nargs="+", required=True)
    parser.add_argument("--iters", type=positive_int, default=50)
    parser.add_argument("--runs", type=positive_int, default=5)
    parser.add_argument("--threads", type=positive_int, default=10)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    vocab = str(root / "assets/rwkv_vocab_v20230424.txt")
    env = dict(os.environ, GOMAXPROCS=str(args.threads), RAYON_NUM_THREADS=str(args.threads))

    def run(command):
        return subprocess.check_output(command, env=env, text=True).strip()

    presets = ["speed", "balanced", "memory"] if args.preset == "all" else [args.preset]
    environment = {"platform": platform.platform(), "go": run(["go", "version"]),
                   "rust": run(["rustc", "--version"])}
    if platform.system() == "Darwin":
        environment["cpu"] = run(["sysctl", "-n", "machdep.cpu.brand_string"])
    source = b"".join(path.name.encode() + path.read_bytes() for path in sorted(root.glob("*.go"))
                      if not path.name.endswith("_test.go"))

    with tempfile.TemporaryDirectory() as tmp:
        for corpus in args.corpus:
            data = Path(corpus).read_bytes()
            if not data:
                parser.error(f"empty corpus: {corpus}")
            run([args.rust, "dump", corpus, vocab, tmp + "/rust.bin"])
            reference = Path(tmp + "/rust.bin").read_bytes()
            for preset in presets:
                run([args.go, "dump", "-in", corpus, "-out", tmp + "/go.bin", "-preset", preset])
                if Path(tmp + "/go.bin").read_bytes() != reference:
                    raise RuntimeError(f"token mismatch: {corpus}, {preset}")
            if args.baseline:
                run([args.baseline, "dump", "-in", corpus, "-out", tmp + "/go.bin"])
                if Path(tmp + "/go.bin").read_bytes() != reference:
                    raise RuntimeError(f"baseline token mismatch: {corpus}")

            for batch in [0, 64]:
                common = ["bench", "-in", corpus, "-iters", str(args.iters),
                          "-warmup", "3", "-batch", str(batch)]
                commands = {"rust": [args.rust, "bench", corpus, vocab, str(args.iters), str(batch)]}
                for preset in presets:
                    key = "go-" + preset if args.preset == "all" else "go"
                    commands[key] = [args.go] + common + ["-preset", preset]
                if args.baseline:
                    commands["baseline"] = [args.baseline] + common
                samples = {key: [] for key in commands}
                token_counts = set()
                for rep in range(args.runs):
                    keys = list(commands)
                    keys = keys[rep % len(keys):] + keys[:rep % len(keys)]
                    for key in keys:
                        output = run(commands[key])
                        samples[key].append(float(re.search(r"([\d.]+) MB/s", output).group(1)))
                        token_counts.add(int(re.search(r"(\d+) tokens", output).group(1)))
                if len(token_counts) != 1:
                    raise RuntimeError(f"token counts differ in timed runs: {token_counts}")
                result = dict(
                    preset=args.preset, corpus=corpus, bytes=len(data),
                    sha256=hashlib.sha256(data).hexdigest(), batch=batch,
                    threads=args.threads, iters=args.iters, warmup=3, runs=args.runs,
                    tokens=token_counts.pop(), environment=environment,
                    go_source_sha256=hashlib.sha256(source).hexdigest(),
                    vocab_sha256=hashlib.sha256(Path(vocab).read_bytes()).hexdigest(),
                    samples=samples, median={key: statistics.median(values) for key, values in samples.items()},
                )
                print(json.dumps(result), flush=True)


if __name__ == "__main__":
    main()
