package rwkvtok

import "testing"

func TestRejectOutOfRangeVocabNumbers(t *testing.T) {
	for _, line := range []string{
		"2147483648 'a' 1",           // exceeds token ID representation
		"4294967296 'a' 1",           // would wrap to zero as int32
		"18446744073709551617 'a' 1", // would wrap to positive int on 64-bit
		"1 'a' 18446744073709551617",
		`1 '\UFFFFFFFF' 3`,
		`1 '\U80000000' 3`,
	} {
		t.Run(line, func(t *testing.T) {
			if _, err := NewFromBytes([]byte(line)); err == nil {
				t.Fatal("out-of-range vocabulary value accepted")
			}
		})
	}
}
