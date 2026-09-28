package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDumpPresets(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(input, []byte("Today is a beautiful day. 今天是美好的一天。"), 0600); err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, preset := range []string{"speed", "balanced", "memory"} {
		output := filepath.Join(dir, preset+".bin")
		if err := cmdDump([]string{"-preset", preset, "-in", input, "-out", output}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if want == nil {
			want = got
		} else if !bytes.Equal(got, want) {
			t.Fatalf("preset %s dump mismatch", preset)
		}
	}
	if err := cmdDump([]string{"-preset", "typo", "-in", input, "-out", filepath.Join(dir, "bad.bin")}); err == nil {
		t.Fatal("invalid preset accepted")
	}
}
