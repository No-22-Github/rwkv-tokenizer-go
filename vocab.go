package rwkv

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"unicode/utf8"
)

// The standard RWKV World vocab (rwkv_vocab_v20230424.txt, 65529 tokens),
// embedded so that New works without any external files. Custom vocab files
// with the same format can be loaded via NewFromFile / NewFromReader.
//
//go:embed assets/rwkv_vocab_v20230424.txt
var defaultVocab embed.FS

const defaultVocabPath = "assets/rwkv_vocab_v20230424.txt"

type vocabEntry struct {
	id    int32
	bytes []byte
}

func loadDefaultVocab() ([]byte, error) {
	b, err := defaultVocab.ReadFile(defaultVocabPath)
	if err != nil {
		return nil, fmt.Errorf("rwkv: reading embedded vocab: %w", err)
	}
	return b, nil
}

// parseVocab parses a vocab file where each line has the form
//
//	<id> (b?)'<python-escaped content>' <byte length>
//
// It returns the entries for trie construction and the token table indexed by
// id (entry 0 is a NUL placeholder, mirroring the reference tokenizer).
func parseVocab(data []byte) (entries []vocabEntry, tokens [][]byte, err error) {
	entries = make([]vocabEntry, 0, 65536)
	tokens = make([][]byte, 1, 65536)
	tokens[0] = []byte{0} // id 0 is unused by the vocab; decode(0) yields NUL

	for lineNo, line := range splitLines(data) {
		id, content, declLen, err := parseVocabLine(line)
		if err != nil {
			return nil, nil, fmt.Errorf("rwkv: vocab line %d: %w", lineNo+1, err)
		}
		if int(id) < len(tokens) && tokens[id] != nil {
			return nil, nil, fmt.Errorf("rwkv: vocab line %d: duplicate id %d", lineNo+1, id)
		}
		if declLen != len(content) {
			return nil, nil, fmt.Errorf("rwkv: vocab line %d: declared length %d != decoded length %d", lineNo+1, declLen, len(content))
		}
		for int(id) >= len(tokens) {
			tokens = append(tokens, nil)
		}
		tokens[id] = content
		entries = append(entries, vocabEntry{id: id, bytes: content})
	}
	return entries, tokens, nil
}

// parseVocabLine parses one `<id> (b?)'content' <len>` line.
func parseVocabLine(line []byte) (id int32, content []byte, declLen int, err error) {
	sp := bytes.IndexByte(line, ' ')
	if sp < 0 {
		return 0, nil, 0, errors.New("missing id separator")
	}
	v, err := atoi(line[:sp])
	if err != nil {
		return 0, nil, 0, fmt.Errorf("bad id: %w", err)
	}
	if v > 1<<31-1 {
		return 0, nil, 0, fmt.Errorf("token id %d exceeds int32 range", v)
	}
	id = int32(v)

	rest := line[sp+1:]
	rest = bytes.TrimRight(rest, " \t\r")

	open := 0
	if len(rest) >= 2 && rest[0] == 'b' && isQuote(rest[1]) {
		open = 1
	}
	if len(rest) < 2 || !isQuote(rest[open]) {
		return 0, nil, 0, errors.New("missing opening quote")
	}
	quote := rest[open]

	last := bytes.LastIndexByte(rest, ' ')
	if last < 0 || last < open+2 {
		return 0, nil, 0, errors.New("missing length field")
	}
	if rest[last-1] != quote {
		return 0, nil, 0, errors.New("missing closing quote")
	}
	inner := rest[open+1 : last-1]

	v, err = atoi(rest[last+1:])
	if err != nil {
		return 0, nil, 0, fmt.Errorf("bad length: %w", err)
	}
	declLen = v

	if open == 1 {
		content, err = decodeByteLiteral(inner)
	} else {
		content, err = unescape(inner)
	}
	if err != nil {
		return 0, nil, 0, err
	}
	return id, content, declLen, nil
}

func isQuote(c byte) bool { return c == '\'' || c == '"' }

// decodeByteLiteral decodes the content of a b'...' token: a sequence of
// \xHH escapes, each producing one raw byte.
func decodeByteLiteral(src []byte) ([]byte, error) {
	dst := make([]byte, 0, len(src)/2+4)
	for k := 0; k < len(src); {
		if k+4 > len(src) || src[k] != '\\' || src[k+1] != 'x' {
			return nil, errors.New("byte token supports only \\xHH escapes")
		}
		hi, err := hexVal(src[k+2])
		if err != nil {
			return nil, err
		}
		lo, err := hexVal(src[k+3])
		if err != nil {
			return nil, err
		}
		dst = append(dst, hi<<4|lo)
		k += 4
	}
	return dst, nil
}

// unescape decodes a Python-repr style string literal body into the UTF-8
// bytes of the token, matching the reference implementation's semantics:
// \\ \' \" \n \r \t pass through, \xHH yields code point U+00HH (UTF-8
// encoded, so values >= 0x80 become two bytes), \uHHHH and \UHHHHHHHH yield
// the encoded code point, and anything else is an error.
func unescape(src []byte) ([]byte, error) {
	dst := make([]byte, 0, len(src)+8)
	for k := 0; k < len(src); {
		c := src[k]
		if c != '\\' {
			dst = append(dst, c)
			k++
			continue
		}
		if k+1 >= len(src) {
			return nil, errors.New("trailing backslash")
		}
		switch e := src[k+1]; e {
		case '\\', '\'', '"':
			dst = append(dst, e)
			k += 2
		case 'n':
			dst = append(dst, '\n')
			k += 2
		case 'r':
			dst = append(dst, '\r')
			k += 2
		case 't':
			dst = append(dst, '\t')
			k += 2
		case 'x':
			if k+4 > len(src) {
				return nil, errors.New("truncated \\x escape")
			}
			hi, err := hexVal(src[k+2])
			if err != nil {
				return nil, err
			}
			lo, err := hexVal(src[k+3])
			if err != nil {
				return nil, err
			}
			dst = utf8.AppendRune(dst, rune(hi<<4|lo))
			k += 4
		case 'u', 'U':
			n := 4
			if e == 'U' {
				n = 8
			}
			if k+2+n > len(src) {
				return nil, fmt.Errorf("truncated \\%c escape", e)
			}
			var r rune
			for m := 0; m < n; m++ {
				h, err := hexVal(src[k+2+m])
				if err != nil {
					return nil, err
				}
				r = r<<4 | rune(h)
			}
			if !utf8.ValidRune(r) {
				return nil, fmt.Errorf("invalid code point U+%04X", r)
			}
			dst = utf8.AppendRune(dst, r)
			k += 2 + n
		default:
			return nil, fmt.Errorf("unsupported escape \\%c", e)
		}
	}
	return dst, nil
}

func hexVal(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex digit %q", c)
}

func atoi(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("empty number")
	}
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, errors.New("not a decimal number")
		}
		digit := int(c - '0')
		if n > (int(^uint(0)>>1)-digit)/10 {
			return 0, errors.New("number overflow")
		}
		n = n*10 + digit
	}
	return n, nil
}

func splitLines(data []byte) [][]byte {
	lines := make([][]byte, 0, 65536)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			lines = append(lines, data)
			break
		}
		lines = append(lines, data[:i])
		data = data[i+1:]
	}
	return lines
}
