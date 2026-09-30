package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSchemaRangeReportsAPIReadinessFloorAndMaximum(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("create schema-range capture pipe: %v", err)
	}
	original := os.Stdout
	defer func() { os.Stdout = original }()
	os.Stdout = write
	err = schemaRange()
	if closeErr := write.Close(); closeErr != nil {
		t.Fatalf("close schema-range capture pipe: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("schemaRange: %v", err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, read); err != nil {
		t.Fatalf("read schema-range output: %v", err)
	}
	if got := output.String(); got != "53 53\n" {
		t.Fatalf("schema-range output = %q, want %q", got, "53 53\n")
	}
}
