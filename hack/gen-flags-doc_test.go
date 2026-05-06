package main

import (
	"bytes"
	"os"
	"testing"
)

func TestGenerate_Golden(t *testing.T) {
	want, err := os.ReadFile("testdata/gen-flags/expected.mdx")
	if err != nil {
		t.Fatalf("read expected: %v", err)
	}
	var got bytes.Buffer
	if err := generate("testdata/gen-flags/flags.go", &got); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(got.Bytes())) {
		t.Fatalf("mismatch:\nwant:\n%s\n\ngot:\n%s", want, got.String())
	}
}
