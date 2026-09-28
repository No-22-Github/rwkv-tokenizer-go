package rwkvtok

import (
	"strings"
	"testing"
)

func benchCorpus(b *testing.B) (*Tokenizer, string) {
	b.Helper()
	tok, err := New()
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	text := readTestdata(b, "bench_mixed.txt")
	if len(text) == 0 {
		b.Fatal("empty corpus")
	}
	return tok, text
}

func BenchmarkEncodeMixed(b *testing.B) {
	tok, text := benchCorpus(b)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ids := tok.Encode(text)
		if len(ids) == 0 {
			b.Fatal("no tokens")
		}
	}
}

func BenchmarkEncodeBatchMixed(b *testing.B) {
	tok, text := benchCorpus(b)
	texts := strings.Split(text, "\n")
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tok.EncodeBatch(texts)
	}
}

func BenchmarkDecodeMixed(b *testing.B) {
	tok, text := benchCorpus(b)
	ids := tok.Encode(text)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tok.DecodeBytes(ids); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := New(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodePresets(b *testing.B) {
	text := readTestdata(b, "bench_mixed.txt")
	for _, preset := range allPresets {
		b.Run(string(preset), func(b *testing.B) {
			tok, err := New(WithPreset(preset))
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(text)))
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(trieStorageBytes(tok.trie)), "trie-bytes")
			for i := 0; i < b.N; i++ {
				if len(tok.Encode(text)) == 0 {
					b.Fatal("no tokens")
				}
			}
		})
	}
}
