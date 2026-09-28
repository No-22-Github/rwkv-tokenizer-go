package rwkvtok

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"
)

var allPresets = []Preset{PresetSpeed, PresetBalanced, PresetMemory}

func trieStorageBytes(tr *trie) uintptr {
	return unsafe.Sizeof(*tr) + uintptr(cap(tr.nodes))*unsafe.Sizeof(trieNode{}) +
		uintptr(cap(tr.direct)+cap(tr.childIdx))*4 + uintptr(cap(tr.childBytes))
}

func TestPresets(t *testing.T) {
	base := newTestTokenizer(t)
	if base.Preset() != PresetSpeed {
		t.Fatalf("default preset = %q", base.Preset())
	}
	inputs := [][]byte{
		nil, {}, {0}, {0xff, 0x80, 0, 0x81},
		[]byte("Today is a beautiful day. 今天是美好的一天。"),
		[]byte(readTestdata(t, "bench_mixed.txt")),
		[]byte(readTestdata(t, "japanese.txt")),
		[]byte(readTestdata(t, "utf8_stress.txt")),
	}
	// Every raw byte and all byte pairs, including invalid UTF-8.
	var pairs []byte
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j++ {
			pairs = append(pairs, byte(i), byte(j))
		}
	}
	inputs = append(inputs, pairs)
	want := base.EncodeBatchBytes(inputs)
	var previousSize uintptr
	for _, preset := range allPresets {
		t.Run(string(preset), func(t *testing.T) {
			tok, err := New(WithPreset(preset))
			if err != nil {
				t.Fatal(err)
			}
			if tok.Preset() != preset {
				t.Fatal(tok.Preset())
			}
			size := trieStorageBytes(tok.trie)
			t.Logf("retained trie storage: %d bytes (%.2f MiB)", size, float64(size)/(1<<20))
			if previousSize != 0 && size >= previousSize {
				t.Fatal("presets do not reduce memory")
			}
			previousSize = size
			gotBatch := tok.EncodeBatchBytes(inputs)
			texts := make([]string, len(inputs))
			for i, data := range inputs {
				texts[i] = string(data)
			}
			gotTextBatch := tok.EncodeBatch(texts)
			for i, data := range inputs {
				for _, ids := range [][]int32{tok.EncodeBytes(data), tok.Encode(string(data)), gotBatch[i], gotTextBatch[i]} {
					if !slices.Equal(ids, want[i]) {
						t.Fatalf("input %d: token mismatch", i)
					}
					decoded, err := tok.DecodeBytes(ids)
					if err != nil || !bytes.Equal(decoded, data) {
						t.Fatalf("input %d: decode error %v", i, err)
					}
				}
				prefix := []int32{123, 456}
				expected := append(slices.Clone(prefix), want[i]...)
				for _, ids := range [][]int32{tok.AppendEncode(slices.Clone(prefix), string(data)), tok.AppendEncodeBytes(slices.Clone(prefix), data)} {
					if !slices.Equal(ids, expected) {
						t.Fatalf("input %d: append mismatch", i)
					}
				}
			}
		})
	}
}

func TestConstructorOptions(t *testing.T) {
	vocab := []byte("1 'a' 1\n2 'ab' 2\n3 'b' 1\n")
	path := filepath.Join(t.TempDir(), "vocab.txt")
	if err := os.WriteFile(path, vocab, 0600); err != nil {
		t.Fatal(err)
	}
	constructors := map[string]func(...Option) (*Tokenizer, error){
		"embedded": New,
		"bytes":    func(opts ...Option) (*Tokenizer, error) { return NewFromBytes(vocab, opts...) },
		"reader":   func(opts ...Option) (*Tokenizer, error) { return NewFromReader(bytes.NewReader(vocab), opts...) },
		"file":     func(opts ...Option) (*Tokenizer, error) { return NewFromFile(path, opts...) },
	}
	for name, constructor := range constructors {
		t.Run(name, func(t *testing.T) {
			for _, preset := range allPresets {
				tok, err := constructor(WithPreset(preset))
				if err != nil {
					t.Fatal(err)
				}
				if tok.Preset() != preset {
					t.Fatal(tok.Preset())
				}
				if name != "embedded" && !slices.Equal(tok.Encode("aba"), []int32{2, 1}) {
					t.Fatal("custom vocab mismatch")
				}
			}
			for _, opts := range [][]Option{{WithPreset("unknown")}, {nil}, {WithPreset(""), WithPreset(PresetSpeed)}} {
				if tok, err := constructor(opts...); err == nil || tok != nil {
					t.Fatalf("invalid options: got %v, %v", tok, err)
				}
			}
			tok, err := constructor(WithPreset(PresetMemory), WithPreset(PresetBalanced))
			if err != nil || tok.Preset() != PresetBalanced {
				t.Fatalf("option precedence: %v", err)
			}
		})
	}
}

func FuzzPresetParity(f *testing.F) {
	for _, input := range []string{"", "abc", "你好，世界😀", strings.Repeat(" ", 128), "\x00\xff\x80"} {
		f.Add([]byte(input))
	}
	var tokenizers []*Tokenizer
	for _, preset := range allPresets {
		tok, err := New(WithPreset(preset))
		if err != nil {
			f.Fatal(err)
		}
		tokenizers = append(tokenizers, tok)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		want := tokenizers[0].EncodeBytes(data)
		for _, tok := range tokenizers[1:] {
			if !slices.Equal(tok.EncodeBytes(data), want) {
				t.Fatalf("preset %q mismatch", tok.Preset())
			}
		}
	})
}
