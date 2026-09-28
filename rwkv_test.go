package rwkv

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
)

func newTestTokenizer(t testing.TB) *Tokenizer {
	t.Helper()
	tok, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tok
}

func TestEncodeBeautifulDay(t *testing.T) {
	tok := newTestTokenizer(t)
	// Golden ids from the reference Rust implementation's own test suite.
	got := tok.Encode("Today is a beautiful day. 今天是美好的一天。")
	want := []int32{33520, 4600, 332, 59219, 21509, 47, 33, 10381, 11639, 13091, 15597, 11685, 14734, 10250, 11639, 10080}
	if !equalInt32(got, want) {
		t.Fatalf("Encode mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	tok := newTestTokenizer(t)
	texts := []string{
		"",
		" ",
		"\n",
		"\r\n\r\n",
		strings.Repeat("\n", 12),
		strings.Repeat(" ", 300),
		strings.Repeat("0123456789", 30),
		"Today is a beautiful day. 今天是美好的一天。",
		readTestdata(t, "japanese.txt"),
		readTestdata(t, "utf8_stress.txt"),
		"😀😃😄😁😆😅😂🤣👨‍👩‍👧‍👦🇺🇸",
		"﷐﷑﷒﷓￾￿퟿􏿿",
		"don't can't won't it's 'quoted' \"double\" b'notbyte'",
		"ΠΟΛΥΤΡΟΠΟΣ κόσμε ᾿Ελευθεριά กษัตริย์  estoy",
	}
	for _, text := range texts {
		ids := tok.Encode(text)
		back, err := tok.Decode(ids)
		if err != nil {
			t.Fatalf("Decode(%q): %v", trunc(text), err)
		}
		if back != text {
			t.Fatalf("round trip mismatch for %q:\n got %q", trunc(text), trunc(back))
		}
	}
}

func TestGoldenEdgeCases(t *testing.T) {
	// Byte-for-byte comparison against ids produced by the reference Rust
	// tokenizer on the same input (see bench/generate.py).
	tok := newTestTokenizer(t)
	data := []byte(readTestdata(t, "edge_cases.txt"))
	golden := []byte(readTestdata(t, "edge_cases_golden.bin"))
	if len(golden) < 4 {
		t.Fatal("golden file too short")
	}
	n := binary.LittleEndian.Uint32(golden)
	want := make([]int32, n)
	for i := range want {
		want[i] = int32(binary.LittleEndian.Uint32(golden[4+4*i:]))
	}
	got := tok.EncodeBytes(data)
	if !equalInt32(got, want) {
		for i := 0; i < min(len(got), len(want)); i++ {
			if got[i] != want[i] {
				t.Fatalf("first mismatch at %d: got %d, want %d (context got %v want %v)",
					i, got[i], want[i], got[max(0, i-6):i+6], want[max(0, i-6):i+6])
			}
		}
		t.Fatalf("length mismatch: got %d ids, want %d", len(got), len(want))
	}
}

func TestVocabInvariants(t *testing.T) {
	tok := newTestTokenizer(t)
	if tok.VocabSize() != 65530 {
		t.Fatalf("VocabSize = %d, want 65530 (65529 tokens + placeholder)", tok.VocabSize())
	}
	// Every byte value must start a token, so greedy matching always advances.
	tokBytes := make([]byte, 256)
	for i := range tokBytes {
		tokBytes[i] = byte(i)
		if ids := tok.EncodeBytes(tokBytes[i : i+1]); len(ids) != 1 {
			t.Fatalf("byte %#02x produced %d ids, want exactly 1", i, len(ids))
		}
	}
	ids := tok.EncodeBytes(tokBytes)
	if len(ids) == 0 {
		t.Fatal("no ids for 256-byte input")
	}
	back, err := tok.DecodeBytes(ids)
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}
	if !bytes.Equal(back, tokBytes) {
		t.Fatal("byte round trip mismatch")
	}
	if _, err := tok.Decode(ids); err == nil {
		t.Fatal("Decode of non-UTF-8 byte tokens should fail")
	}
	if b, err := tok.DecodeBytes([]int32{0}); err != nil || !bytes.Equal(b, []byte{0}) {
		t.Fatalf("decode id 0: %v, %v", b, err)
	}
	if _, err := tok.DecodeBytes([]int32{-1}); err == nil {
		t.Fatal("negative id should fail")
	}
	if _, err := tok.DecodeBytes([]int32{int32(tok.VocabSize())}); err == nil {
		t.Fatal("out-of-range id should fail")
	}
}

func TestEncodeBatchMatchesSequential(t *testing.T) {
	tok := newTestTokenizer(t)
	texts := strings.Split(readTestdata(t, "edge_cases.txt"), "\n")
	got := tok.EncodeBatch(texts)
	if len(got) != len(texts) {
		t.Fatalf("EncodeBatch returned %d results for %d texts", len(got), len(texts))
	}
	for i, text := range texts {
		want := tok.Encode(text)
		if !equalInt32(got[i], want) {
			t.Fatalf("EncodeBatch[%d] mismatch:\n got %v\nwant %v", i, got[i], want)
		}
	}
}

func TestEncodeBytesMatchesEncode(t *testing.T) {
	tok := newTestTokenizer(t)
	data := []byte(readTestdata(t, "japanese.txt"))
	if !equalInt32(tok.EncodeBytes(data), tok.Encode(string(data))) {
		t.Fatal("EncodeBytes and Encode disagree")
	}
}

func TestAppendReuse(t *testing.T) {
	tok := newTestTokenizer(t)
	dst := make([]int32, 0, 8)
	dst = tok.AppendEncode(dst, "Today")
	dst = tok.AppendEncode(dst, " is a beautiful day.")
	if want := tok.Encode("Today is a beautiful day."); !equalInt32(dst, want) {
		t.Fatalf("AppendEncode accumulation mismatch:\n got %v\nwant %v", dst, want)
	}
}

func TestConcurrentUse(t *testing.T) {
	tok := newTestTokenizer(t)
	text := readTestdata(t, "japanese.txt")
	want := tok.Encode(text)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got := tok.Encode(text); !equalInt32(got, want) {
					panic("concurrent Encode mismatch")
				}
			}
		}()
	}
	wg.Wait()
}

func FuzzEncodeDecodeBytes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("Today is a beautiful day. 今天是美好的一天。"))
	f.Add(bytes.Repeat([]byte{0x00}, 300))
	f.Add([]byte{0xff, 0xfe, 0x80, 0x81, 0x0a, 0x0a, 0x0a})
	f.Add([]byte(readTestdata(f, "utf8_stress.txt")))
	tok, err := New()
	if err != nil {
		f.Fatalf("New: %v", err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ids := tok.EncodeBytes(data)
		back, err := tok.DecodeBytes(ids)
		if err != nil {
			t.Fatalf("DecodeBytes: %v", err)
		}
		if !bytes.Equal(back, data) {
			t.Fatalf("round trip mismatch for %q", data)
		}
		// Greedy matching must always advance: ids cover the input exactly.
		total := 0
		for _, id := range ids {
			total += len(tok.tokens[id])
		}
		if total != len(data) {
			t.Fatalf("ids cover %d bytes, input has %d", total, len(data))
		}
	})
}

func TestRandomRoundTrip(t *testing.T) {
	tok := newTestTokenizer(t)
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune("01 \n\r\t,.;!?:'\"-=你好 motors")

	for i := 0; i < 200; i++ {
		n := rng.Intn(500)
		// Mix random runes with raw bytes (which may form invalid UTF-8).
		var b []byte
		for j := 0; j < n; j++ {
			if rng.Intn(2) == 0 {
				b = append(b, byte(rng.Intn(256)))
			} else {
				b = append(b, []byte(string(alphabet[rng.Intn(len(alphabet))]))...)
			}
		}
		ids := tok.EncodeBytes(b)
		back, err := tok.DecodeBytes(ids)
		if err != nil || !bytes.Equal(back, b) {
			t.Fatalf("round trip %d failed: %v", i, err)
		}
	}
}

func equalInt32(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readTestdata(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return string(b)
}

func trunc(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
