// Package rwkvtok implements the RWKV "World" tokenizer used by RWKV v5 and
// newer models.
//
// It is a pure-Go port of the reference Rust implementation at
// github.com/cahya-wirawan/rwkv-tokenizer and produces identical token ids:
// text is greedily split into the longest tokens found in the vocab trie,
// with the vocab's byte tokens (< 0x80 as plain ASCII tokens, >= 0x80 as
// b'\xNN' tokens) guaranteeing that every input byte can be represented.
//
// The standard vocab (rwkv_vocab_v20230424.txt, 65529 tokens) is embedded in
// the package, so New works out of the box:
//
//	tok, err := rwkvtok.New()
//	ids := tok.Encode("Today is a beautiful day. 今天是美好的一天。")
//	text, err := tok.Decode(ids)
//
// Tokenization is single-threaded per Encode call and allocation-light;
// EncodeBatch parallelizes across texts on all available cores, and
// AppendEncode / AppendEncodeBytes let callers reuse the output buffer.
package rwkvtok

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

// Tokenizer encodes text to RWKV World token ids and decodes ids back.
// It is safe for concurrent use.
type Tokenizer struct {
	trie   *trie
	tokens [][]byte // indexed by token id; entry 0 is a NUL placeholder
}

// New returns a Tokenizer using the embedded vocab and optional settings.
// The default preset is PresetSpeed.
func New(opts ...Option) (*Tokenizer, error) {
	b, err := loadDefaultVocab()
	if err != nil {
		return nil, err
	}
	return NewFromBytes(b, opts...)
}

// NewFromFile returns a Tokenizer using the vocab file at path.
func NewFromFile(path string, opts ...Option) (*Tokenizer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rwkvtok: %w", err)
	}
	return NewFromBytes(b, opts...)
}

// NewFromReader returns a Tokenizer reading a vocab file from r.
func NewFromReader(r io.Reader, opts ...Option) (*Tokenizer, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("rwkvtok: %w", err)
	}
	return NewFromBytes(b, opts...)
}

// NewFromBytes returns a Tokenizer parsing a vocab file held in memory.
func NewFromBytes(data []byte, opts ...Option) (*Tokenizer, error) {
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	entries, tokens, err := parseVocab(data)
	if err != nil {
		return nil, err
	}
	return &Tokenizer{trie: buildTrie(entries, o.preset), tokens: tokens}, nil
}

// Encode tokenizes text into RWKV World token ids.
func (t *Tokenizer) Encode(text string) []int32 {
	return t.AppendEncode(nil, text)
}

// AppendEncode tokenizes text, appending the ids to dst. The string's bytes
// are viewed without copying; they are only read, never retained.
func (t *Tokenizer) AppendEncode(dst []int32, text string) []int32 {
	if len(text) == 0 {
		return dst
	}
	// Realistic text yields ~0.1-0.5 tokens per byte; the estimate avoids
	// most growth reallocations without badly over-allocating on outliers.
	dst = growForEncode(dst, len(text)/3+8)
	return t.trie.encode(dst, unsafe.Slice(unsafe.StringData(text), len(text)))
}

// EncodeBytes tokenizes raw bytes, which need not be valid UTF-8: bytes
// outside any multi-byte token are covered by the vocab's byte tokens.
func (t *Tokenizer) EncodeBytes(data []byte) []int32 {
	return t.AppendEncodeBytes(nil, data)
}

// AppendEncodeBytes tokenizes raw bytes, appending the ids to dst.
func (t *Tokenizer) AppendEncodeBytes(dst []int32, data []byte) []int32 {
	if len(data) == 0 {
		return dst
	}
	dst = growForEncode(dst, len(data)/3+8)
	return t.trie.encode(dst, data)
}

// growForEncode ensures dst has room for about n more int32 values.
func growForEncode(dst []int32, n int) []int32 {
	if cap(dst)-len(dst) >= n {
		return dst
	}
	buf := make([]int32, len(dst), len(dst)+n+64)
	copy(buf, dst)
	return buf
}

// EncodeBatch tokenizes texts in parallel across all cores, preserving order.
func (t *Tokenizer) EncodeBatch(texts []string) [][]int32 {
	out := make([][]int32, len(texts))
	batch(len(texts), func(i int) { out[i] = t.AppendEncode(nil, texts[i]) })
	return out
}

// EncodeBatchBytes is EncodeBatch for raw byte inputs.
func (t *Tokenizer) EncodeBatchBytes(datas [][]byte) [][]int32 {
	out := make([][]int32, len(datas))
	batch(len(datas), func(i int) { out[i] = t.trie.encode(nil, datas[i]) })
	return out
}

// batch runs fn(0..n-1) in parallel, distributing indices dynamically so that
// unevenly sized items stay balanced.
func batch(n int, fn func(int)) {
	if n == 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// Decode joins the tokens for ids and returns the resulting text. It fails if
// any id is not in the vocab or the joined bytes are not valid UTF-8.
func (t *Tokenizer) Decode(ids []int32) (string, error) {
	b, err := t.DecodeBytes(ids)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("rwkvtok: decoded token sequence is not valid UTF-8")
	}
	return string(b), nil
}

// DecodeBytes joins the tokens for ids without UTF-8 validation.
func (t *Tokenizer) DecodeBytes(ids []int32) ([]byte, error) {
	tokens := t.tokens
	total := 0
	for _, id := range ids {
		if id < 0 || int(id) >= len(tokens) || tokens[id] == nil {
			return nil, fmt.Errorf("rwkvtok: invalid token id %d", id)
		}
		total += len(tokens[id])
	}
	buf := make([]byte, 0, total)
	for _, id := range ids {
		buf = append(buf, tokens[id]...)
	}
	return buf, nil
}

// VocabSize returns the number of token slots, vocab id 0 (a NUL placeholder)
// included.
func (t *Tokenizer) VocabSize() int { return len(t.tokens) }

// Preset returns the immutable layout selected when the tokenizer was created.
func (t *Tokenizer) Preset() Preset { return t.trie.preset }
