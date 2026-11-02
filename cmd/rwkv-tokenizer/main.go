// Command rwkv-tokenizer is a CLI for the RWKV World tokenizer: it encodes
// text to token ids, decodes ids back to text, counts tokens, dumps ids in a
// binary format for cross-implementation checks, and benchmarks throughput.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	rwkvtok "github.com/No-22-Github/rwkv-tokenizer-go"
)

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rwkv-tokenizer:", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "encode":
		err = cmdEncode(os.Args[2:])
	case "decode":
		err = cmdDecode(os.Args[2:])
	case "count":
		err = cmdCount(os.Args[2:])
	case "dump":
		err = cmdDump(os.Args[2:])
	case "bench":
		err = cmdBench(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: rwkv-tokenizer <command> [flags]

commands:
  encode [-vocab FILE] [-in FILE]      print space-separated token ids for the input text
  decode [-vocab FILE] [-in FILE]      print the text for whitespace-separated token ids
  count  [-vocab FILE] [-in FILE] [-eod] [-jsonl]
                                       print the token count; with -jsonl, copy JSON Lines
                                       adding a <field>_tok count per string field
  dump   [-vocab FILE] -in FILE -out FILE
                                       write ids as binary (uint32 count, then uint32 LE ids)
  bench  [-vocab FILE] -in FILE [-iters N] [-batch K] [-warmup W]
                                       measure encode throughput over the input text

flags:
  -preset  speed (default), balanced, or memory
  -vocab   vocab file (default: embedded rwkv_vocab_v20230424.txt)
  -in      input file (default: stdin)
`)
}

func loadTokenizer(vocabPath, preset string) (*rwkvtok.Tokenizer, error) {
	if vocabPath == "" {
		return rwkvtok.New(rwkvtok.WithPreset(rwkvtok.Preset(preset)))
	}
	return rwkvtok.NewFromFile(vocabPath, rwkvtok.WithPreset(rwkvtok.Preset(preset)))
}

func readInput(path string) ([]byte, error) {
	if path == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func tokenFlagSet() (*flag.FlagSet, *string, *string, *string) {
	fs := flag.NewFlagSet("", flag.ExitOnError)
	vocab := fs.String("vocab", "", "vocab file (default: embedded)")
	in := fs.String("in", "", "input file (default: stdin)")
	preset := fs.String("preset", "speed", "trie preset: speed, balanced, memory")
	return fs, vocab, in, preset
}

func cmdEncode(args []string) error {
	fs, vocab, in, preset := tokenFlagSet()
	fs.Parse(args)
	tok, err := loadTokenizer(*vocab, *preset)
	if err != nil {
		return err
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	ids := tok.AppendEncodeBytes(make([]int32, 0, len(data)/3+16), data)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for i, id := range ids {
		if i > 0 {
			w.WriteByte(' ')
		}
		w.WriteString(strconv.Itoa(int(id)))
	}
	return w.Flush()
}

func cmdCount(args []string) error {
	fs, vocabPath, in, preset := tokenFlagSet()
	eod := fs.Bool("eod", false, `encode the text "<EOD>" as token 0`)
	jsonl := fs.Bool("jsonl", false, "copy JSON Lines, adding <field>_tok per string field")
	fs.Parse(args)
	opts := []rwkvtok.Option{rwkvtok.WithPreset(rwkvtok.Preset(*preset))}
	if *eod {
		opts = append(opts, rwkvtok.WithEOD())
	}
	var tok *rwkvtok.Tokenizer
	var err error
	if *vocabPath == "" {
		tok, err = rwkvtok.New(opts...)
	} else {
		tok, err = rwkvtok.NewFromFile(*vocabPath, opts...)
	}
	if err != nil {
		return err
	}
	if *jsonl {
		src := io.Reader(os.Stdin)
		if *in != "" {
			f, err := os.Open(*in)
			if err != nil {
				return err
			}
			defer f.Close()
			src = f
		}
		return tok.CountJSONL(os.Stdout, src)
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	fmt.Println(tok.CountBytes(data))
	return nil
}

func cmdDecode(args []string) error {
	fs, vocab, in, preset := tokenFlagSet()
	fs.Parse(args)
	tok, err := loadTokenizer(*vocab, *preset)
	if err != nil {
		return err
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	ids := make([]int32, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseInt(f, 10, 32)
		if err != nil {
			return fmt.Errorf("bad token id %q: %w", f, err)
		}
		ids = append(ids, int32(v))
	}
	text, err := tok.Decode(ids)
	if err != nil {
		return err
	}
	_, err = os.Stdout.WriteString(text)
	return err
}

func cmdDump(args []string) error {
	fs, vocab, in, preset := tokenFlagSet()
	out := fs.String("out", "", "output file")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("dump requires -out")
	}
	tok, err := loadTokenizer(*vocab, *preset)
	if err != nil {
		return err
	}
	data, err := readInput(*in)
	if err != nil {
		return err
	}
	ids := tok.EncodeBytes(data)
	buf := make([]byte, 4+4*len(ids))
	putU32(buf, uint32(len(ids)))
	for i, id := range ids {
		putU32(buf[4+4*i:], uint32(id))
	}
	return os.WriteFile(*out, buf, 0o644)
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func cmdBench(args []string) error {
	fs, vocab, in, preset := tokenFlagSet()
	iters := fs.Int("iters", 10, "timed iterations")
	warmup := fs.Int("warmup", 1, "warmup iterations")
	batchN := fs.Int("batch", 0, "if > 0, split the input into this many chunks and encode them in parallel")
	fs.Parse(args)

	t0 := time.Now()
	tok, err := loadTokenizer(*vocab, *preset)
	if err != nil {
		return err
	}
	load := time.Since(t0)

	data, err := readInput(*in)
	if err != nil {
		return err
	}
	if *batchN <= 0 {
		benchSingle(tok, data, *warmup, *iters, load)
	} else {
		benchBatch(tok, data, *warmup, *iters, *batchN, load)
	}
	return nil
}

func benchSingle(tok *rwkvtok.Tokenizer, data []byte, warmup, iters int, load time.Duration) {
	text := string(data) // one-time conversion; Encode views strings without copying
	var total int
	for range warmup {
		total = len(tok.Encode(text))
	}
	start := time.Now()
	for range iters {
		total = len(tok.Encode(text))
	}
	elapsed := time.Since(start)
	report("encode (single-thread)", load, len(data), total, iters, elapsed, runtime.GOMAXPROCS(0), tok.Preset())
}

func benchBatch(tok *rwkvtok.Tokenizer, data []byte, warmup, iters, chunks int, load time.Duration) {
	texts := splitChunks(data, chunks)
	var total int
	for range warmup {
		total = 0
		for _, ids := range tok.EncodeBatch(texts) {
			total += len(ids)
		}
	}
	start := time.Now()
	for range iters {
		total = 0
		for _, ids := range tok.EncodeBatch(texts) {
			total += len(ids)
		}
	}
	elapsed := time.Since(start)
	report(fmt.Sprintf("encode-batch (%d chunks)", chunks), load, len(data), total, iters, elapsed, runtime.GOMAXPROCS(0), tok.Preset())
}

// splitChunks cuts data into n chunks at newline boundaries, preserving UTF-8.
// Tokens can span newlines, so batch tokenization may differ from a single text.
func splitChunks(data []byte, n int) []string {
	if n <= 1 || len(data) < 2*n {
		return []string{string(data)}
	}
	size := len(data) / n
	cuts := make([]int, 1, n+1)
	cuts[0] = 0
	for len(cuts) < n {
		want := cuts[len(cuts)-1] + size
		nl := want
		for nl < len(data) && data[nl] != '\n' {
			nl++
		}
		if nl >= len(data) {
			break
		}
		cuts = append(cuts, nl+1)
	}
	cuts = append(cuts, len(data))
	texts := make([]string, 0, len(cuts)-1)
	for i := 0; i+1 < len(cuts); i++ {
		texts = append(texts, string(data[cuts[i]:cuts[i+1]]))
	}
	return texts
}

func report(name string, load time.Duration, bytes, tokens, iters int, elapsed time.Duration, threads int, preset rwkvtok.Preset) {
	perIter := elapsed / time.Duration(iters)
	mbps := float64(bytes) * float64(iters) / 1e6 / elapsed.Seconds()
	mtoks := float64(tokens) * float64(iters) / 1e6 / elapsed.Seconds()
	fmt.Printf("%-28s %9d bytes  %9d tokens  %10s/iter  %8.1f MB/s  %8.1f Mtok/s  (preset %s, load %s, %d threads)\n",
		name, bytes, tokens, perIter.Round(time.Microsecond), mbps, mtoks, preset, load.Round(time.Millisecond), threads)
}
