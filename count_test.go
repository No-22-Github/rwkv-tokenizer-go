package rwkvtok

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func newEODTokenizer(t testing.TB) *Tokenizer {
	t.Helper()
	tok, err := New(WithEOD(), WithPreset(PresetBalanced))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tok
}

func TestCountMatchesEncode(t *testing.T) {
	tok := newTestTokenizer(t)
	for _, text := range []string{"", "Today is a beautiful day. 今天是美好的一天。", "a\x80b"} {
		want := len(tok.Encode(text))
		if got := tok.Count(text); got != want {
			t.Errorf("Count(%q) = %d, want %d", text, got, want)
		}
		if got := tok.CountBytes([]byte(text)); got != want {
			t.Errorf("CountBytes(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestCountDoesNotAllocatePerCall(t *testing.T) {
	tok := newTestTokenizer(t)
	text := strings.Repeat("Today is a beautiful day. 今天是美好的一天。", 20)
	tok.Count(text)
	if allocs := testing.AllocsPerRun(100, func() { tok.Count(text) }); allocs > 1 {
		t.Fatalf("Count allocated %.0f times per call", allocs)
	}
}

// Ids from the rwkv_lightning trie (rwkv_trie.hpp), which carries "<EOD>" as
// token 0: only where greedy matching lands on it, since ".<", "<<" and " <"
// are longer tokens that straddle the marker.
func TestEODMatchesLightningTrie(t *testing.T) {
	pins := map[string][]int32{
		"<EOD><EOD>":                      {0, 0},
		"a<EOD>b":                         {98, 0, 99},
		".<EOD>":                          {589, 979, 69, 63},
		"<<EOD>>":                         {755, 979, 69, 792},
		"x <EOD>\n\nUser:":                {121, 295, 979, 69, 63, 261, 24281, 59},
		"</EOD><EOD>EOD>":                 {754, 979, 69, 790, 979, 69, 63, 979, 69, 63},
		strings.Repeat("-", 32) + "<EOD>": {65458, 0},
	}
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemory} {
		tok, err := New(WithEOD(), WithPreset(preset))
		if err != nil {
			t.Fatal(err)
		}
		for text, want := range pins {
			if got := tok.Encode(text); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: Encode(%q) = %v, want %v", preset, text, got, want)
			}
			if got := tok.Count(text); got != len(want) {
				t.Errorf("%s: Count(%q) = %d, want %d", preset, text, got, len(want))
			}
		}
	}
}

func TestEODRoundTrip(t *testing.T) {
	tok := newEODTokenizer(t)
	const text = "before<EOD>after"
	got, err := tok.Decode(tok.Encode(text))
	if err != nil || got != text {
		t.Fatalf("round trip = %q, %v", got, err)
	}
}

func TestWithoutEODMarkerIsPlainText(t *testing.T) {
	tok := newTestTokenizer(t)
	if got := tok.Count("<EOD>"); got <= 1 {
		t.Fatalf(`Count("<EOD>") = %d without WithEOD, want the plain vocab split`, got)
	}
}

func TestEODFloodIsLinear(t *testing.T) {
	tok := newEODTokenizer(t)
	const markers = 4 << 20 / len("<EOD>")
	if got := tok.Count(strings.Repeat("<EOD>", markers)); got != markers {
		t.Fatalf("marker flood = %d, want %d", got, markers)
	}
	if got := tok.Count(strings.Repeat("a<EOD>", 100000)); got != 200000 {
		t.Fatalf("interleaved flood = %d, want 200000", got)
	}
}

func TestVocabSHA256AndDefaultVocab(t *testing.T) {
	data, err := DefaultVocab()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	want := hex.EncodeToString(digest[:])
	if want != "e6dee3d4e31b4d5c40ac99508ac6c701ceef4bed681bf2167ce9a908552bca89" {
		t.Fatalf("embedded vocab digest changed: %s", want)
	}
	if got := newTestTokenizer(t).VocabSHA256(); got != want {
		t.Fatalf("VocabSHA256 = %s, want %s", got, want)
	}
	custom, err := NewFromBytes([]byte("1 'a' 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if custom.VocabSHA256() == want {
		t.Fatal("custom vocab reports the embedded digest")
	}
}

func TestCountJSONL(t *testing.T) {
	tok := newTestTokenizer(t)
	input := `{"text":"<think>hi</think>","id":12345678901234567,"meta":{"inner":"x"},"text_tok":99}` + "\n" +
		`{"n":1}` + "\n"
	var out bytes.Buffer
	if err := tok.CountJSONL(&out, strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	want := `{"id":12345678901234567,"meta":{"inner":"x"},"text":"<think>hi</think>","text_tok":` +
		itoa(tok.Count("<think>hi</think>")) + "}\n" + `{"n":1}` + "\n"
	if out.String() != want {
		t.Fatalf("got  %s\nwant %s", out.String(), want)
	}
}

func TestCountJSONLRejectsBadLines(t *testing.T) {
	tok := newTestTokenizer(t)
	for name, input := range map[string]string{
		"not json":   "{\"a\":\"b\"}\nnope\n",
		"null":       "{\"a\":\"b\"}\nnull\n",
		"array":      "{\"a\":\"b\"}\n[1]\n",
		"two values": "{\"a\":\"b\"}\n{\"c\":1} {\"d\":2}\n",
		"blank":      "{\"a\":\"b\"}\n\n",
	} {
		err := tok.CountJSONL(&bytes.Buffer{}, strings.NewReader(input))
		if err == nil || !strings.Contains(err.Error(), "line 2") {
			t.Errorf("%s: err = %v, want a line 2 error", name, err)
		}
	}
}

func itoa(n int) string {
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
