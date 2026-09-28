package rwkvtok

import "fmt"

// Preset selects the memory/throughput tradeoff of the immutable encoding trie.
// All presets produce the same token IDs and support the same vocabularies.
type Preset string

const (
	// PresetSpeed uses direct lookup tables for every branching node.
	// It is the default and uses the most trie memory.
	PresetSpeed Preset = "speed"
	// PresetBalanced uses direct tables only for high-fanout nodes, scanning
	// small sorted edge lists for the other branching nodes.
	PresetBalanced Preset = "balanced"
	// PresetMemory stores every branching node as a sorted edge list, using
	// binary search for large lists. It uses the least trie memory.
	PresetMemory Preset = "memory"
)

type tokenizerOptions struct {
	preset Preset
}

// Option configures a Tokenizer at construction time.
type Option func(*tokenizerOptions) error

// WithPreset selects a trie layout. The default is PresetSpeed. If supplied
// more than once, the last valid option wins. An unknown preset is an error.
func WithPreset(preset Preset) Option {
	return func(o *tokenizerOptions) error {
		switch preset {
		case PresetSpeed, PresetBalanced, PresetMemory:
			o.preset = preset
			return nil
		default:
			return fmt.Errorf("rwkvtok: unknown preset %q (want speed, balanced, or memory)", preset)
		}
	}
}

func resolveOptions(opts []Option) (tokenizerOptions, error) {
	o := tokenizerOptions{preset: PresetSpeed}
	for _, opt := range opts {
		if opt == nil {
			return o, fmt.Errorf("rwkvtok: nil option")
		}
		if err := opt(&o); err != nil {
			return o, err
		}
	}
	return o, nil
}
