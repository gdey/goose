package goose

import "testing"

func Test_getExtension(t *testing.T) {

	fn := func(filename, eExt string) func(t *testing.T) {
		return func(t *testing.T) {
			ext := getExtension(filename)
			if ext != eExt {
				t.Errorf("ext, expected %s got %s", eExt, ext)
			}
		}
	}
	tests := map[string]string{
		"foo":         "",
		"foo.sql":     ".sql",
		"foo.tpl.sql": ".tpl.sql",
		"foo.go":      ".go",
		"github.com/gdey/goose/migrations/foo.go":      ".go",
		"github.com/gdey/goose/migrations/foo.sql":     ".sql",
		"github.com/gdey/goose/migrations/foo.tpl.sql": ".tpl.sql",
		"github.com/gdey/goose/migrations":             "",
	}
	for name, ext := range tests {
		t.Run(name, fn(name, ext))
	}

}
