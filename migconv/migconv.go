// Package migconv converts SQL migrations between the golang-migrate
// layout (a pair of "NNN_name.up.sql" / "NNN_name.down.sql" files) and
// the goose layout (a single "NNN_name.sql" file with "-- +goose Up"
// and "-- +goose Down" markers).
//
// Every exported function here takes plain strings (filenames and file
// contents) and returns plain strings or a Migration value. None of them
// touch the filesystem, so they can be tested with ordinary table tests
// and reused by any caller, not just the CLI in cmd/migconv.
package migconv

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	gooseUpMarker   = "-- +goose Up"
	gooseDownMarker = "-- +goose Down"
)

// Migration is the format-neutral representation both converters read
// from and write to.
type Migration struct {
	Version string // e.g. "20240115093000" or "000001"
	Name    string // e.g. "create_users_table"
	Up      string // SQL body applied when migrating forward
	Down    string // SQL body applied when rolling back
}

var (
	golangMigrateRe = regexp.MustCompile(`^([0-9]+)_(.+)\.(up|down)\.sql$`)
	gooseFileRe     = regexp.MustCompile(`^([0-9]+)_(.+)\.sql$`)
)

// ParseGolangMigrateFilename splits a golang-migrate style filename into
// its version, name, and direction ("up" or "down").
func ParseGolangMigrateFilename(filename string) (version, name, direction string, err error) {
	m := golangMigrateRe.FindStringSubmatch(filename)
	if m == nil {
		return "", "", "", fmt.Errorf("migconv: %q does not look like a golang-migrate filename (want NNN_name.up.sql or NNN_name.down.sql)", filename)
	}
	return m[1], m[2], m[3], nil
}

// FromGolangMigrate combines an up file and a down file, as produced by
// golang-migrate, into a single Migration. The two filenames must share
// the same version and name and carry opposite directions.
func FromGolangMigrate(upFilename, upContent, downFilename, downContent string) (Migration, error) {
	upVersion, upName, upDir, err := ParseGolangMigrateFilename(upFilename)
	if err != nil {
		return Migration{}, err
	}
	if upDir != "up" {
		return Migration{}, fmt.Errorf("migconv: %q is not an up migration", upFilename)
	}

	downVersion, downName, downDir, err := ParseGolangMigrateFilename(downFilename)
	if err != nil {
		return Migration{}, err
	}
	if downDir != "down" {
		return Migration{}, fmt.Errorf("migconv: %q is not a down migration", downFilename)
	}

	if upVersion != downVersion || upName != downName {
		return Migration{}, fmt.Errorf("migconv: %q and %q do not belong to the same migration", upFilename, downFilename)
	}

	return Migration{
		Version: upVersion,
		Name:    upName,
		Up:      upContent,
		Down:    downContent,
	}, nil
}

// ToGolangMigrate renders a Migration as the pair of files golang-migrate
// expects: filename and contents for the up side, then for the down side.
func ToGolangMigrate(m Migration) (upFilename, upContent, downFilename, downContent string) {
	upFilename = fmt.Sprintf("%s_%s.up.sql", m.Version, m.Name)
	downFilename = fmt.Sprintf("%s_%s.down.sql", m.Version, m.Name)
	return upFilename, m.Up, downFilename, m.Down
}

// ParseGooseFilename splits a goose style filename into its version and
// name.
func ParseGooseFilename(filename string) (version, name string, err error) {
	m := gooseFileRe.FindStringSubmatch(filename)
	if m == nil {
		return "", "", fmt.Errorf("migconv: %q does not look like a goose filename (want NNN_name.sql)", filename)
	}
	return m[1], m[2], nil
}

// ParseGoose reads a single goose migration file and splits it into a
// Migration using the "-- +goose Up" / "-- +goose Down" markers.
//
// It does not special-case "-- +goose StatementBegin" / "StatementEnd"
// blocks: their lines are kept verbatim inside Up or Down, which is
// enough for round-tripping but loses the statement-boundary hint those
// markers exist to give goose.
func ParseGoose(filename, content string) (Migration, error) {
	version, name, err := ParseGooseFilename(filename)
	if err != nil {
		return Migration{}, err
	}

	upIdx := strings.Index(content, gooseUpMarker)
	if upIdx == -1 {
		return Migration{}, fmt.Errorf("migconv: %q is missing the %q marker", filename, gooseUpMarker)
	}

	downIdx := strings.Index(content, gooseDownMarker)
	if downIdx == -1 {
		return Migration{}, fmt.Errorf("migconv: %q is missing the %q marker", filename, gooseDownMarker)
	}
	if downIdx < upIdx {
		return Migration{}, fmt.Errorf("migconv: %q has %q before %q", filename, gooseDownMarker, gooseUpMarker)
	}

	up := content[upIdx+len(gooseUpMarker) : downIdx]
	down := content[downIdx+len(gooseDownMarker):]

	return Migration{
		Version: version,
		Name:    name,
		Up:      strings.Trim(up, "\n") + "\n",
		Down:    strings.Trim(down, "\n") + "\n",
	}, nil
}

// FormatGoose renders a Migration as a single goose migration file.
func FormatGoose(m Migration) (filename, content string) {
	filename = fmt.Sprintf("%s_%s.sql", m.Version, m.Name)

	var b strings.Builder
	b.WriteString(gooseUpMarker)
	b.WriteString("\n")
	b.WriteString(strings.Trim(m.Up, "\n"))
	b.WriteString("\n\n")
	b.WriteString(gooseDownMarker)
	b.WriteString("\n")
	b.WriteString(strings.Trim(m.Down, "\n"))
	b.WriteString("\n")

	return filename, b.String()
}
