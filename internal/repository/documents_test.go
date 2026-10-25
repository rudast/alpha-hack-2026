package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

func TestDocumentStore(t *testing.T) {
	root := t.TempDir()
	s, err := NewDocumentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	app := "8c1f0000-0000-0000-0000-000000000001"

	if docs, _ := s.List(app); len(docs) != 0 {
		t.Fatalf("no folder must mean no documents, got %v", docs)
	}
	doc, err := s.Save(app, "passport", "паспорт.pdf", strings.NewReader("%PDF-1.7"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Path != app+"/passport/паспорт.pdf" || doc.Size != 8 {
		t.Fatalf("got %+v", doc)
	}
	if _, err := os.Stat(filepath.Join(root, app, "passport", "паспорт.pdf")); err != nil {
		t.Fatalf("file is not in the volume layout: %v", err)
	}
	_, _ = s.Save(app, "account_statement", "выписка.pdf", strings.NewReader("%PDF-1.7 x"))

	docs, err := s.List(app)
	if err != nil || len(docs) != 2 || docs[0].Kind != "account_statement" || docs[1].Kind != "passport" {
		t.Fatalf("got %+v, %v", docs, err)
	}

	if err := s.Delete(app, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(app, doc.ID); !errors.Is(err, domain.ErrDocumentNotFound) {
		t.Fatalf("second delete: got %v", err)
	}
}

func TestDocumentStore_RejectsPathsOutsideTheFolder(t *testing.T) {
	s, _ := NewDocumentStore(t.TempDir())
	for _, tc := range [][3]string{
		{"../app", "passport", "a.pdf"},
		{"app", "..", "a.pdf"},
		{"app", "passport", "../a.pdf"},
		{"app", "passport", ".hidden"},
	} {
		if _, err := s.Save(tc[0], tc[1], tc[2], strings.NewReader("x")); !errors.Is(err, domain.ErrInvalidDocument) {
			t.Errorf("%v: got %v, want ErrInvalidDocument", tc, err)
		}
	}
	if err := s.Delete("app", "not-base64!"); !errors.Is(err, domain.ErrDocumentNotFound) {
		t.Errorf("bad id: got %v", err)
	}
}
