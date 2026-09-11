package migconv

import (
	"strings"
	"testing"
)

func TestFromGolangMigrateThenToGolangMigrate(t *testing.T) {
	m, err := FromGolangMigrate(
		"000001_create_users.up.sql", "CREATE TABLE users (id INTEGER);\n",
		"000001_create_users.down.sql", "DROP TABLE users;\n",
	)
	if err != nil {
		t.Fatalf("FromGolangMigrate: %v", err)
	}

	upFilename, upContent, downFilename, downContent := ToGolangMigrate(m)
	if upFilename != "000001_create_users.up.sql" {
		t.Errorf("upFilename = %q", upFilename)
	}
	if downFilename != "000001_create_users.down.sql" {
		t.Errorf("downFilename = %q", downFilename)
	}
	if upContent != "CREATE TABLE users (id INTEGER);\n" {
		t.Errorf("upContent = %q", upContent)
	}
	if downContent != "DROP TABLE users;\n" {
		t.Errorf("downContent = %q", downContent)
	}
}

func TestFromGolangMigrateMismatchedPair(t *testing.T) {
	_, err := FromGolangMigrate(
		"000001_create_users.up.sql", "CREATE TABLE users (id INTEGER);\n",
		"000002_create_posts.down.sql", "DROP TABLE posts;\n",
	)
	if err == nil {
		t.Fatal("expected an error for mismatched up/down filenames, got nil")
	}
}

func TestParseGoose(t *testing.T) {
	content := "-- +goose Up\n" +
		"CREATE TABLE users (id INTEGER);\n" +
		"\n" +
		"-- +goose Down\n" +
		"DROP TABLE users;\n"

	m, err := ParseGoose("000001_create_users.sql", content)
	if err != nil {
		t.Fatalf("ParseGoose: %v", err)
	}
	if m.Version != "000001" || m.Name != "create_users" {
		t.Errorf("version/name = %q/%q", m.Version, m.Name)
	}
	if m.Up != "CREATE TABLE users (id INTEGER);\n" {
		t.Errorf("Up = %q", m.Up)
	}
	if m.Down != "DROP TABLE users;\n" {
		t.Errorf("Down = %q", m.Down)
	}
}

func TestParseGooseMissingMarker(t *testing.T) {
	_, err := ParseGoose("000001_create_users.sql", "CREATE TABLE users (id INTEGER);\n")
	if err == nil {
		t.Fatal("expected an error for a file with no goose markers, got nil")
	}
}

func TestFormatGooseWrapsDollarQuotedFunctionBody(t *testing.T) {
	up := "CREATE FUNCTION set_updated_at() RETURNS trigger AS $$\n" +
		"BEGIN\n" +
		"  NEW.updated_at = now();\n" +
		"  RETURN NEW;\n" +
		"END;\n" +
		"$$ LANGUAGE plpgsql;\n"

	_, content := FormatGoose(Migration{
		Version: "000001",
		Name:    "set_updated_at",
		Up:      up,
		Down:    "DROP FUNCTION set_updated_at();\n",
	})

	if !strings.Contains(content, gooseStatementBegin) || !strings.Contains(content, gooseStatementEnd) {
		t.Fatalf("expected StatementBegin/StatementEnd markers around the function body, got:\n%s", content)
	}

	begin := strings.Index(content, gooseStatementBegin)
	end := strings.Index(content, gooseStatementEnd)
	if begin == -1 || end == -1 || end < begin {
		t.Fatalf("StatementBegin/StatementEnd markers out of order in:\n%s", content)
	}
	body := content[begin:end]
	if !strings.Contains(body, "NEW.updated_at = now();") {
		t.Errorf("function body missing from wrapped statement:\n%s", body)
	}

	// The single-statement Down body must not be wrapped.
	if strings.Count(content, gooseStatementBegin) != 1 {
		t.Errorf("expected exactly one StatementBegin marker, got content:\n%s", content)
	}
}

func TestFormatGooseDoesNotDoubleWrapExistingMarkers(t *testing.T) {
	up := gooseStatementBegin + "\n" +
		"CREATE FUNCTION set_updated_at() RETURNS trigger AS $$\n" +
		"BEGIN\n" +
		"  NEW.updated_at = now();\n" +
		"  RETURN NEW;\n" +
		"END;\n" +
		"$$ LANGUAGE plpgsql;\n" +
		gooseStatementEnd + "\n"

	_, content := FormatGoose(Migration{
		Version: "000001",
		Name:    "set_updated_at",
		Up:      up,
		Down:    "DROP FUNCTION set_updated_at();\n",
	})

	if strings.Count(content, gooseStatementBegin) != 1 {
		t.Errorf("expected an already-marked statement to keep exactly one StatementBegin marker, got:\n%s", content)
	}
}

func TestFormatGooseDoesNotSplitOnSemicolonInsideStringLiteral(t *testing.T) {
	up := "INSERT INTO notes (body) VALUES ('a; b; c');\n"

	_, content := FormatGoose(Migration{
		Version: "000001",
		Name:    "seed_notes",
		Up:      up,
		Down:    "DELETE FROM notes;\n",
	})

	if strings.Contains(content, gooseStatementBegin) {
		t.Errorf("a semicolon inside a string literal should not trigger StatementBegin/StatementEnd wrapping, got:\n%s", content)
	}
}

func TestGolangMigrateToGooseRoundTrip(t *testing.T) {
	original := Migration{
		Version: "000001",
		Name:    "create_users",
		Up:      "CREATE TABLE users (id INTEGER);\n",
		Down:    "DROP TABLE users;\n",
	}

	filename, content := FormatGoose(original)
	parsed, err := ParseGoose(filename, content)
	if err != nil {
		t.Fatalf("ParseGoose: %v", err)
	}
	if parsed != original {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", parsed, original)
	}
}
