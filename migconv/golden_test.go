package migconv

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenGolangMigrateToGoose converts full golang-migrate up/down files
// (with comments, multi-line formatting, and more than one statement per
// side - the shape real migrations actually take) and checks the result
// against a checked-in goose file, catching regressions in FormatGoose's
// statement splitting and joining that a synthetic one-liner wouldn't.
func TestGoldenGolangMigrateToGoose(t *testing.T) {
	cases := []string{"000001_create_users"}

	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			upFilename := base + ".up.sql"
			downFilename := base + ".down.sql"
			upContent := readGolden(t, "golang_migrate", upFilename)
			downContent := readGolden(t, "golang_migrate", downFilename)

			m, err := FromGolangMigrate(upFilename, upContent, downFilename, downContent)
			if err != nil {
				t.Fatalf("FromGolangMigrate: %v", err)
			}

			wantFilename := base + ".sql"
			want := readGolden(t, "goose", wantFilename)

			gotFilename, got := FormatGoose(m)
			if gotFilename != wantFilename {
				t.Errorf("filename = %q, want %q", gotFilename, wantFilename)
			}
			if got != want {
				t.Errorf("FormatGoose output mismatch:\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestGoldenGooseToGolangMigrate parses a hand-written goose file that
// already carries StatementBegin/StatementEnd markers around a
// dollar-quoted trigger function, and checks that ParseGoose plus
// ToGolangMigrate reproduce exactly the up/down files golang-migrate needs,
// markers and blank lines included.
func TestGoldenGooseToGolangMigrate(t *testing.T) {
	cases := []string{"000002_add_updated_at_trigger"}

	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			filename := base + ".sql"
			content := readGolden(t, "goose", filename)

			m, err := ParseGoose(filename, content)
			if err != nil {
				t.Fatalf("ParseGoose: %v", err)
			}

			wantUpFilename := base + ".up.sql"
			wantDownFilename := base + ".down.sql"
			wantUp := readGolden(t, "golang_migrate", wantUpFilename)
			wantDown := readGolden(t, "golang_migrate", wantDownFilename)

			upFilename, upContent, downFilename, downContent := ToGolangMigrate(m)
			if upFilename != wantUpFilename {
				t.Errorf("upFilename = %q, want %q", upFilename, wantUpFilename)
			}
			if downFilename != wantDownFilename {
				t.Errorf("downFilename = %q, want %q", downFilename, wantDownFilename)
			}
			if upContent != wantUp {
				t.Errorf("up content mismatch:\n got:\n%s\nwant:\n%s", upContent, wantUp)
			}
			if downContent != wantDown {
				t.Errorf("down content mismatch:\n got:\n%s\nwant:\n%s", downContent, wantDown)
			}
		})
	}
}

func readGolden(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", dir, name))
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	return string(b)
}
