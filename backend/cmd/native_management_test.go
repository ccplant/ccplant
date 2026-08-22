package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestTailFileAndReleaseVersionValidation(t *testing.T) {
	path := t.TempDir() + "/native.log"
	if err := atomicWriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := tailFile(path, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "two" || lines[1] != "three" {
		t.Fatalf("tailFile() = %#v", lines)
	}
	for value, want := range map[string]bool{"v1.20.0": true, "v1.20.0-rc.1": true, "latest": false, "v1/../../x": false} {
		if got := validReleaseVersion(value); got != want {
			t.Errorf("validReleaseVersion(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestExtractNativeBinary(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	payload := []byte("binary")
	if err := tw.WriteHeader(&tar.Header{Name: "ccplant", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	got, err := extractNativeBinary(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extractNativeBinary() = %q", got)
	}
}
