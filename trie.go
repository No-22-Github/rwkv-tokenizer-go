package rwkv

import "sort"

// Trie layout
//
// All nodes live in one flat slice and reference children through int32
// indices, so the whole structure is a few compact scalar arrays — invisible
// to the garbage collector and cache-friendly, which matters because the
// encode loop walks it millions of times per second.
//
// Single-child nodes inline their edge; leaves use the same representation
// with an impossible edge byte (-1). Every branching node gets a 256-entry
// table in speed mode. Balanced and memory modes store some or all branching
// nodes as sorted sparse edges. Speed mode keeps its own two-case hot loop;
// the preset is dispatched once per encode call, not once per byte.
// Nodes remain 16 bytes for cheap indexed access.
type trie struct {
	preset     Preset
	childBytes []byte
	childIdx   []int32
	pairs      [65536]int32 // depth-2 node index, -1 if absent
	nodes      []trieNode
	root       [256]int32 // depth-1 node index, -1 if absent
	direct     []int32    // 256-entry child tables, with -1 for absent edges
}

type trieNode struct {
	id   int32 // terminating token ID, -1 if none
	x    int32 // single: child node; hub: direct offset; sparse: first edge
	y    int32 // single: edge byte (-1 for leaf); sparse: end edge; hub: unused
	kind int32
}

const (
	nodeKindScan   = 0
	nodeKindSingle = 1
	nodeKindHub    = 2
	nodeKindSearch = 3
)

// build-node used during construction; replaced by trieNode after flattening.
type tnode struct {
	b       byte
	child   int32 // first child, -1 if none
	last    int32 // last child, -1 if none
	sibling int32 // next child of the same parent, -1 if none
	id      int32 // token id, -1 if none
}

// buildTrie builds the trie from (id, bytes) entries. Entries with duplicate
// bytes keep the highest id, matching the reference implementation where a
// later insert overwrites the id at a node.
func buildTrie(entries []vocabEntry, preset Preset) *trie {
	// Sorting lexicographically (id as tiebreaker) turns insertion into a
	// depth-first walk of the final tree: consecutive entries share their
	// longest common prefix, so children of any node are created in ascending
	// byte order and the sibling chains come out pre-sorted.
	sort.Slice(entries, func(i, j int) bool {
		if c := compareBytes(entries[i].bytes, entries[j].bytes); c != 0 {
			return c < 0
		}
		return entries[i].id < entries[j].id
	})

	// The node count is bounded by 1 + total token bytes, so one allocation
	// is enough; children are linked by index, never by pointer, because
	// append would otherwise strand writes into a reallocated backing array.
	total := 1
	for _, e := range entries {
		total += len(e.bytes)
	}
	tnodes := make([]tnode, 1, total)
	tnodes[0] = tnode{child: -1, last: -1, sibling: -1, id: -1}
	// stack holds the node indices of the path to the current prefix;
	// stack[d+1] is the node for prefix byte d.
	stack := make([]int32, 1, 136)
	stack[0] = 0
	var prev []byte
	for _, e := range entries {
		w := e.bytes
		if len(w) == 0 {
			continue
		}
		lcp := 0
		for lcp < len(w) && lcp < len(prev) && w[lcp] == prev[lcp] {
			lcp++
		}
		stack = stack[:lcp+1]
		for d := lcp; d < len(w); d++ {
			p := stack[len(stack)-1]
			idx := int32(len(tnodes))
			tnodes = append(tnodes, tnode{b: w[d], child: -1, last: -1, sibling: -1, id: -1})
			if tnodes[p].child < 0 {
				tnodes[p].child = idx
			} else {
				tnodes[tnodes[p].last].sibling = idx
			}
			tnodes[p].last = idx
			stack = append(stack, idx)
		}
		tnodes[stack[len(stack)-1]].id = e.id
		prev = w
	}

	threshold := 1
	switch preset {
	case PresetBalanced:
		threshold = 8
	case PresetMemory:
		threshold = 256
	}
	hubs, edges := 0, 0
	for i := 1; i < len(tnodes); i++ {
		count := 0
		for c := tnodes[i].child; c >= 0; c = tnodes[c].sibling {
			count++
		}
		if count > threshold {
			hubs++
		} else if count > 1 {
			edges += count
		}
	}
	t := &trie{
		preset:     preset,
		nodes:      make([]trieNode, len(tnodes)),
		direct:     make([]int32, 0, hubs*256),
		childBytes: make([]byte, 0, edges),
		childIdx:   make([]int32, 0, edges),
	}
	for i := range t.root {
		t.root[i] = -1
	}
	for i := range t.pairs {
		t.pairs[i] = -1
	}
	for c := tnodes[0].child; c >= 0; c = tnodes[c].sibling {
		for d := tnodes[c].child; d >= 0; d = tnodes[d].sibling {
			t.pairs[int(tnodes[c].b)<<8|int(tnodes[d].b)] = d
		}
		t.root[tnodes[c].b] = c
	}
	for c := tnodes[0].child; c >= 0; c = tnodes[c].sibling {
		flatten(tnodes, t, c, threshold)
	}
	t.nodes[0] = trieNode{id: -1, y: -1, kind: nodeKindSingle} // root; only entered via t.root
	return t
}

// flatten writes the edge data of node n and its subtree.
func flatten(tnodes []tnode, t *trie, n int32, threshold int) {
	nd := &tnodes[n]

	// Count children to pick the representation.
	count := 0
	for c := nd.child; c >= 0; c = tnodes[c].sibling {
		count++
	}

	node := trieNode{id: nd.id}
	switch {
	case count == 0:
		node.kind = nodeKindSingle
		node.y = -1
	case count == 1:
		c := nd.child
		node.kind = nodeKindSingle
		node.x = c // child indices are always >= 1
		node.y = int32(tnodes[c].b)
	case count > threshold:
		// Hub node: a private 256-entry table indexed by the next byte.
		// Absent children are -1 (miss); a zero fill would alias the root.
		node.kind = nodeKindHub
		node.x = int32(len(t.direct))
		table := make([]int32, 256)
		for i := range table {
			table[i] = -1
		}
		for c := nd.child; c >= 0; c = tnodes[c].sibling {
			table[tnodes[c].b] = c
		}
		t.direct = append(t.direct, table...)
	default:
		node.kind = nodeKindScan
		if count > 8 {
			node.kind = nodeKindSearch
		}
		node.x = int32(len(t.childBytes))
		for c := nd.child; c >= 0; c = tnodes[c].sibling {
			t.childBytes = append(t.childBytes, tnodes[c].b)
			t.childIdx = append(t.childIdx, c)
		}
		node.y = int32(len(t.childBytes))
	}
	t.nodes[n] = node

	for c := nd.child; c >= 0; c = tnodes[c].sibling {
		flatten(tnodes, t, c, threshold)
	}
}

// compareBytes is bytes.Compare without the import in the hot build path.
func compareBytes(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// encode appends the token ids for data to out and returns the extended slice.
//
// It is a faithful port of the reference tokenizer's greedy longest match: at
// each position it descends the trie as far as the input allows, emits the id
// of the deepest node that terminates a token, and repeats. If no token starts
// with the byte at the current position, tokenization stops and the ids
// collected so far are returned — the reference behaves the same way, and with
// the standard vocab every byte value starts a token, so it cannot happen.
func (t *trie) encode(out []int32, data []byte) []int32 {
	if t.preset == PresetSpeed {
		return t.encodeSpeed(out, data)
	}
	return t.encodeSparse(out, data)
}

func (t *trie) encodeSpeed(out []int32, data []byte) []int32 {
	nodes := t.nodes
	direct := t.direct
	root := &t.root
	i, n := 0, len(data)
	for i < n {
		cur := root[data[i]] // data[i] is a byte: the compiler proves this in range
		if cur < 0 {
			return out
		}
		id := nodes[cur].id
		matchLen := 1
		j := i + 1
		// Jump over the first two bytes with one lookup, retaining the
		// one-byte fallback when the pair is only an internal prefix.
		if j < n {
			next := t.pairs[uint16(data[i])<<8|uint16(data[j])]
			if next < 0 {
				if id < 0 {
					return out
				}
				out = append(out, id)
				i++
				continue
			}
			cur = next
			j++
			if nid := nodes[cur].id; nid >= 0 {
				id = nid
				matchLen = 2
			}
		}
		nd := nodes[cur]
		for j < n {
			b := data[j]
			next := int32(-1)
			if nd.kind == nodeKindSingle {
				// Single child, with the edge inlined in the node.
				if nd.y == int32(b) {
					next = nd.x
				}
			} else {
				next = direct[nd.x+int32(b)]
			}
			if next < 0 {
				break
			}
			nd = nodes[next]
			j++
			if nid := nd.id; nid >= 0 {
				id = nid
				matchLen = j - i
			}
		}
		if id < 0 {
			return out
		}
		out = append(out, id)
		i += matchLen
	}
	return out
}

// encodeSparse keeps sparse edge handling out of the speed preset hot loop.
func (t *trie) encodeSparse(out []int32, data []byte) []int32 {
	nodes := t.nodes
	direct := t.direct
	cbytes, cidx := t.childBytes, t.childIdx
	root := &t.root
	i, n := 0, len(data)
	for i < n {
		cur := root[data[i]] // data[i] is a byte: the compiler proves this in range
		if cur < 0 {
			return out
		}
		id := nodes[cur].id
		matchLen := 1
		j := i + 1
		// Jump over the first two bytes with one lookup, retaining the
		// one-byte fallback when the pair is only an internal prefix.
		if j < n {
			next := t.pairs[uint16(data[i])<<8|uint16(data[j])]
			if next < 0 {
				if id < 0 {
					return out
				}
				out = append(out, id)
				i++
				continue
			}
			cur = next
			j++
			if nid := nodes[cur].id; nid >= 0 {
				id = nid
				matchLen = 2
			}
		}
		nd := nodes[cur]
		for j < n {
			b := data[j]
			next := int32(-1)
			if nd.kind == nodeKindSingle {
				// Single child, with the edge inlined in the node.
				if nd.y == int32(b) {
					next = nd.x
				}
			} else if nd.kind == nodeKindHub {
				next = direct[nd.x+int32(b)]
			} else if nd.kind == nodeKindSearch {
				lo, hi := nd.x, nd.y
				for lo < hi {
					mid := lo + (hi-lo)/2
					if cbytes[mid] < b {
						lo = mid + 1
					} else {
						hi = mid
					}
				}
				if lo < nd.y && cbytes[lo] == b {
					next = cidx[lo]
				}
			} else {
				for k := nd.x; k < nd.y; k++ {
					if cbytes[k] == b {
						next = cidx[k]
						break
					}
				}
			}
			if next < 0 {
				break
			}
			nd = nodes[next]
			j++
			if nid := nd.id; nid >= 0 {
				id = nid
				matchLen = j - i
			}
		}
		if id < 0 {
			return out
		}
		out = append(out, id)
		i += matchLen
	}
	return out
}
