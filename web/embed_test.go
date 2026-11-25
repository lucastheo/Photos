package web_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/lucastheo/photos/web"
)

func TestDistFS(t *testing.T) {
	t.Parallel()

	fsys, err := web.DistFS()
	if err != nil {
		t.Fatalf("DistFS() failed: %v", err)
	}
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(data), "<photo-tracker-app>") {
		t.Fatalf("unexpected index.html contents: %q", string(data))
	}
}
