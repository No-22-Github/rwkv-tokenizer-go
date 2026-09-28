package rwkvtok

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"
)

// TestTrieIntegrity walks the byte path of every vocab token through the trie
// and checks it terminates at a node carrying the right id. It also checks
// every node's kind discriminator is valid.
func TestTrieIntegrity(t *testing.T) {
	for _, preset := range allPresets {
		t.Run(string(preset), func(t *testing.T) { checkTrieIntegrity(t, preset) })
	}
}

func checkTrieIntegrity(t *testing.T, preset Preset) {
	tok, err := New(WithPreset(preset))
	if err != nil {
		t.Fatal(err)
	}
	for id, b := range tok.trie.nodes {
		switch b.kind {
		case nodeKindSingle, nodeKindHub, nodeKindScan, nodeKindSearch:
		default:
			t.Fatalf("node %d has invalid kind %d", id, b.kind)
		}
	}
	for id, b := range tok.tokens {
		if id == 0 || len(b) == 0 {
			continue
		}
		cur := tok.trie.root[b[0]]
		if cur < 0 {
			t.Fatalf("token %d (%q): root has no child for %#02x", id, b, b[0])
		}
		for k := 1; k < len(b); k++ {
			nd := tok.trie.nodes[cur]
			next := int32(-1)
			switch nd.kind {
			case nodeKindSingle:
				if nd.y == int32(b[k]) {
					next = nd.x
				}
			case nodeKindHub:
				next = tok.trie.direct[nd.x+int32(b[k])]
			case nodeKindScan, nodeKindSearch:
				for j := nd.x; j < nd.y; j++ {
					if tok.trie.childBytes[j] == b[k] {
						next = tok.trie.childIdx[j]
						break
					}
				}
			}
			if next < 0 {
				t.Fatalf("token %d (%q): trie path broken at byte %d", id, b, k)
			}
			cur = next
		}
		if tok.trie.nodes[cur].id != int32(id) {
			t.Fatalf("token %d (%q): trie path ends at node with id %d", id, b, tok.trie.nodes[cur].id)
		}
	}
}

// The pair shortcut must preserve the last terminal node when a longer
// prefix fails, including sparse custom vocabularies without byte fallbacks.
func TestPairPrefixFallback(t *testing.T) {
	for _, preset := range allPresets {
		t.Run(string(preset), func(t *testing.T) { checkPairPrefixFallback(t, preset) })
	}
}

func checkPairPrefixFallback(t *testing.T, preset Preset) {
	tr := buildTrie([]vocabEntry{
		{id: 1, bytes: []byte("a")},
		{id: 2, bytes: []byte("abc")},
		{id: 3, bytes: []byte("b")},
		{id: 4, bytes: []byte("x")},
	}, preset)
	for _, tc := range []struct {
		text string
		want []int32
	}{
		{"", nil}, {"a", []int32{1}}, {"ab", []int32{1, 3}},
		{"abx", []int32{1, 3, 4}}, {"abcx", []int32{2, 4}},
		{"ax", []int32{1, 4}}, {"zabc", nil}, {"azabc", []int32{1}},
	} {
		got := tr.encode(nil, []byte(tc.text))
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%q: got %v, want %v", tc.text, got, tc.want)
		}
	}
	tr = buildTrie([]vocabEntry{{id: 1, bytes: []byte("abc")}}, preset)
	if got := tr.encode(nil, []byte("abx")); len(got) != 0 {
		t.Fatal(got)
	}
}

// Compare with a deliberately simple vocabulary scan, independent of the trie
// representation. Sparse vocabularies exercise stopping and longest-match
// fallback, which round-trip tests with the standard byte-complete vocab miss.
func TestTrieAgainstGreedyOracle(t *testing.T) {
	for _, preset := range allPresets {
		t.Run(string(preset), func(t *testing.T) { checkTrieAgainstGreedyOracle(t, preset) })
	}
}

func checkTrieAgainstGreedyOracle(t *testing.T, preset Preset) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 100; trial++ {
		entries := make([]vocabEntry, 0, 80)
		for i := 0; i < 80; i++ {
			word := make([]byte, 1+rng.Intn(8))
			for j := range word {
				word[j] = byte(rng.Intn(8))
			}
			entries = append(entries, vocabEntry{id: int32(i + 1), bytes: word})
		}
		tr := buildTrie(entries, preset)
		inputs := [][]byte{nil, {255}, {0, 255}}
		for _, e := range entries {
			inputs = append(inputs, e.bytes)
		}
		for i := 0; i < 80; i++ {
			var input []byte
			for j := 0; j < 12; j++ {
				input = append(input, entries[rng.Intn(len(entries))].bytes...)
			}
			inputs = append(inputs, input)
		}
		for _, input := range inputs {
			var want []int32
			for remaining := input; len(remaining) > 0; {
				bestLen, bestID := 0, int32(-1)
				for _, e := range entries {
					if bytes.HasPrefix(remaining, e.bytes) && (len(e.bytes) > bestLen || len(e.bytes) == bestLen && e.id > bestID) {
						bestLen, bestID = len(e.bytes), e.id
					}
				}
				if bestLen == 0 {
					break
				}
				want = append(want, bestID)
				remaining = remaining[bestLen:]
			}
			if got := tr.encode(nil, input); !slices.Equal(got, want) {
				t.Fatalf("trial %d input %x: got %v, want %v", trial, input, got, want)
			}
		}
	}
}

func TestBranchFallbackPresets(t *testing.T) {
	entries := []vocabEntry{{id: 1, bytes: []byte("a")}, {id: 2, bytes: []byte("ab")}}
	for b := 0; b < 256; b += 2 {
		entries = append(entries, vocabEntry{id: int32(1000 + b), bytes: []byte{'a', 'b', byte(b), 'x'}})
	}
	for _, preset := range allPresets {
		t.Run(string(preset), func(t *testing.T) {
			tr := buildTrie(slices.Clone(entries), preset)
			for b := 0; b < 256; b++ {
				for _, suffix := range [][]byte{nil, {'x'}, {255}} {
					input := append([]byte{'a', 'b', byte(b)}, suffix...)
					// Odd third bytes miss the sparse list; even ones may
					// match only a nonterminal prefix and must fall back.
					want := []int32{2}
					if b%2 == 0 && len(suffix) != 0 && suffix[0] == 'x' {
						want = []int32{int32(1000 + b)}
					} else if b == 'a' {
						want = []int32{2, 1}
					}
					if got := tr.encode(nil, input); !slices.Equal(got, want) {
						t.Fatalf("%x: got %v want %v", input, got, want)
					}
				}
			}
		})
	}
}
