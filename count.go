package rwkvtok

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Count returns the number of tokens text encodes to, reusing an internal
// output buffer so that repeated counting does not allocate per call.
func (t *Tokenizer) Count(text string) int {
	buf := t.countBuffer()
	*buf = t.AppendEncode((*buf)[:0], text)
	n := len(*buf)
	t.bufs.Put(buf)
	return n
}

// CountBytes is Count for raw bytes, which need not be valid UTF-8.
func (t *Tokenizer) CountBytes(data []byte) int {
	buf := t.countBuffer()
	*buf = t.AppendEncodeBytes((*buf)[:0], data)
	n := len(*buf)
	t.bufs.Put(buf)
	return n
}

func (t *Tokenizer) countBuffer() *[]int32 {
	if buf, ok := t.bufs.Get().(*[]int32); ok {
		return buf
	}
	return new([]int32)
}

// maxJSONLLine bounds one input line of CountJSONL.
const maxJSONLLine = 1 << 30

// CountJSONL copies JSON Lines from src to dst, adding a "<field>_tok" count
// for every top-level string field of each object. Other fields pass through
// (numbers keep their exact text); keys come out sorted. An existing field of
// the same name is overwritten. Each row is flushed as it is written, so a
// caller can use this as a line-at-a-time coprocess. A malformed line stops the
// copy with an error naming its line number.
func (t *Tokenizer) CountJSONL(dst io.Writer, src io.Reader) error {
	out := bufio.NewWriter(dst)
	// Keep "<think>" and "<tool_call>" readable instead of < escapes.
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 1<<20), maxJSONLLine)
	for line := 1; scanner.Scan(); line++ {
		row, err := decodeJSONLRow(scanner.Bytes())
		if err != nil {
			return fmt.Errorf("rwkvtok: line %d: %w", line, err)
		}
		counts := make(map[string]int, len(row))
		for key, value := range row {
			if text, ok := value.(string); ok {
				counts[key+"_tok"] = t.Count(text)
			}
		}
		for key, n := range counts {
			row[key] = n
		}
		if err := encoder.Encode(row); err != nil {
			return fmt.Errorf("rwkvtok: line %d: %w", line, err)
		}
		if err := out.Flush(); err != nil {
			return fmt.Errorf("rwkvtok: line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("rwkvtok: %w", err)
	}
	return nil
}

func decodeJSONLRow(line []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("not a JSON object")
	}
	if decoder.More() {
		return nil, errors.New("trailing data after JSON object")
	}
	return row, nil
}
