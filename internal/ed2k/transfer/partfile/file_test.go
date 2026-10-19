package partfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func metaForTest(t *testing.T, body []byte) (*Meta, []byte) {
	t.Helper()
	hashes, err := HashReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	return NewMeta(hashes.File, hashes.Parts, "sample.bin", int64(len(body)), 99), body
}

func TestPartFileWriteVerifyAndReopen(t *testing.T) {
	body := []byte("hello ed2k")
	meta, data := metaForTest(t, body)
	partPath := filepath.Join(t.TempDir(), "sample.part")
	p, err := Create(partPath, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WritePart(0, data); err != nil {
		t.Fatal(err)
	}
	if !p.Complete() {
		t.Fatal("file should be complete")
	}
	ok, err := p.Verify()
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v", ok, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partPath + ".met"); err != nil {
		t.Fatalf("part.met missing: %v", err)
	}
	reopened, err := Open(partPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Have(0) || !reopened.Complete() {
		t.Fatalf("reopened have/complete = %v/%v", reopened.Have(0), reopened.Complete())
	}
}

func TestPartFileRejectsWrongHash(t *testing.T) {
	body := []byte("hello ed2k")
	meta, _ := metaForTest(t, body)
	p, err := Create(filepath.Join(t.TempDir(), "sample.part"), meta)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.WritePart(0, []byte("HELLO ED2K")); err == nil {
		t.Fatal("wrong part must be rejected")
	}
	if p.Have(0) || p.Complete() {
		t.Fatal("a rejected part must stay missing")
	}
}

func TestPartFileTwoPartsAndResume(t *testing.T) {
	body := bytes.Repeat([]byte("x"), int(PartSize)+10)
	meta, data := metaForTest(t, body)
	partPath := filepath.Join(t.TempDir(), "two.part")
	p, err := Create(partPath, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WritePart(0, data[:PartSize]); err != nil {
		t.Fatal(err)
	}
	if p.Complete() {
		t.Fatal("second part is still missing")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Have(0) || reopened.Have(1) || reopened.Complete() {
		t.Fatalf("resume bitmap = %v/%v/%v", reopened.Have(0), reopened.Have(1), reopened.Complete())
	}
	if err := reopened.WritePart(1, data[PartSize:]); err != nil {
		t.Fatal(err)
	}
	ok, err := reopened.Verify()
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v", ok, err)
	}
	reopened.Close()
}
