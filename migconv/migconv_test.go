package migconv

import "testing"

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
