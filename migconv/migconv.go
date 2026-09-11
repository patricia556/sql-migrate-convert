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
	gooseUpMarker          = "-- +goose Up"
	gooseDownMarker        = "-- +goose Down"
	gooseStatementBegin    = "-- +goose StatementBegin"
	gooseStatementEnd      = "-- +goose StatementEnd"
	gooseStatementMarkerID = "+goose Statement"
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
// "-- +goose StatementBegin" / "StatementEnd" blocks are kept verbatim
// inside Up or Down rather than unwrapped: golang-migrate has no use for
// them, but leaving them in place is harmless (they're SQL comments) and
// preserves the exact statement the author protected. FormatGoose is the
// side that has to reconstruct these markers when they aren't already
// present, since it's the direction that can turn a plain SQL body into
// something goose would otherwise mis-split.
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
//
// Any statement in Up or Down that goose's own semicolon-splitting
// would cut in the wrong place - typically a dollar-quoted function or
// trigger body with semicolons of its own - is wrapped in
// "-- +goose StatementBegin" / "StatementEnd" markers so goose runs it
// as one statement instead of several broken fragments.
func FormatGoose(m Migration) (filename, content string) {
	filename = fmt.Sprintf("%s_%s.sql", m.Version, m.Name)

	var b strings.Builder
	b.WriteString(gooseUpMarker)
	b.WriteString("\n")
	b.WriteString(protectGooseStatements(m.Up))
	b.WriteString("\n\n")
	b.WriteString(gooseDownMarker)
	b.WriteString("\n")
	b.WriteString(protectGooseStatements(m.Down))
	b.WriteString("\n")

	return filename, b.String()
}

// sqlStatement is one top-level statement produced by splitSQLStatements.
type sqlStatement struct {
	text string
	// needsProtection is true if the statement contains a dollar-quoted
	// body with a semicolon inside it - the case goose can't split
	// correctly without a StatementBegin/StatementEnd wrapper. It is not
	// set for semicolons inside ordinary string literals or comments,
	// since goose's own splitter already skips over those.
	needsProtection bool
}

// protectGooseStatements splits sql into top-level statements and wraps
// any statement flagged by splitSQLStatements as needing protection in
// goose's StatementBegin/StatementEnd markers.
//
// Statements that already carry a "+goose Statement" marker are left
// alone, so re-formatting a Migration that came from ParseGoose (and so
// may already contain hand-placed markers) doesn't nest them.
func protectGooseStatements(sql string) string {
	stmts := splitSQLStatements(sql)

	var b strings.Builder
	for _, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt.text)
		if trimmed == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if !stmt.needsProtection || strings.Contains(trimmed, gooseStatementMarkerID) {
			b.WriteString(trimmed)
			continue
		}
		b.WriteString(gooseStatementBegin)
		b.WriteString("\n")
		b.WriteString(trimmed)
		b.WriteString("\n")
		b.WriteString(gooseStatementEnd)
	}
	return b.String()
}

// splitSQLStatements splits sql into top-level statements on ";",
// without splitting inside single- or double-quoted strings, "--" and
// "/* */" comments, or dollar-quoted bodies (Postgres's "$$ ... $$" or
// "$tag$ ... $tag$" syntax, used to write function and trigger bodies
// that contain their own semicolons). The trailing ";" is kept as part
// of the statement it terminates.
func splitSQLStatements(sql string) []sqlStatement {
	var stmts []sqlStatement
	var cur strings.Builder
	protect := false
	n := len(sql)

	for i := 0; i < n; {
		switch c := sql[i]; {
		case c == '\'' || c == '"':
			j := skipQuoted(sql, i, c)
			cur.WriteString(sql[i:j])
			i = j
		case c == '-' && i+1 < n && sql[i+1] == '-':
			j := strings.IndexByte(sql[i:], '\n')
			if j == -1 {
				j = n
			} else {
				j = i + j + 1
			}
			cur.WriteString(sql[i:j])
			i = j
		case c == '/' && i+1 < n && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			var j int
			if end == -1 {
				j = n
			} else {
				j = i + 2 + end + 2
			}
			cur.WriteString(sql[i:j])
			i = j
		case c == '$':
			j := skipDollarQuoted(sql, i)
			if strings.Contains(sql[i:j], ";") {
				protect = true
			}
			cur.WriteString(sql[i:j])
			i = j
		case c == ';':
			cur.WriteByte(c)
			stmts = append(stmts, sqlStatement{text: cur.String(), needsProtection: protect})
			cur.Reset()
			protect = false
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, sqlStatement{text: cur.String(), needsProtection: protect})
	}
	return stmts
}

// skipQuoted returns the index just past the end of a '...' or "..."
// string starting at start, treating a doubled quote character as an
// escaped literal quote rather than the end of the string.
func skipQuoted(sql string, start int, quote byte) int {
	n := len(sql)
	i := start + 1
	for i < n {
		if sql[i] == quote {
			if i+1 < n && sql[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return n
}

// skipDollarQuoted returns the index just past a Postgres dollar-quoted
// body ("$$ ... $$" or "$tag$ ... $tag$") starting at start. If sql[start]
// isn't the start of a valid dollar-quote tag, it returns start+1 so the
// caller treats the "$" as an ordinary character.
func skipDollarQuoted(sql string, start int) int {
	n := len(sql)
	j := start + 1
	for j < n && isDollarTagByte(sql[j]) {
		j++
	}
	if j >= n || sql[j] != '$' {
		return start + 1
	}
	tag := sql[start : j+1]

	closeIdx := strings.Index(sql[j+1:], tag)
	if closeIdx == -1 {
		return n
	}
	return j + 1 + closeIdx + len(tag)
}

func isDollarTagByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
