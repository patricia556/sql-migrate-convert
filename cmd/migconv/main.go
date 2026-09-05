// Command migconv converts a directory of SQL migrations between the
// golang-migrate layout and the goose layout using the migconv package.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/patricia556/sql-migrate-convert/migconv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migconv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("migconv", flag.ContinueOnError)
	from := fs.String("from", "", `source format: "golang-migrate" or "goose"`)
	to := fs.String("to", "", `target format: "golang-migrate" or "goose"`)
	in := fs.String("in", "", "directory to read migrations from")
	out := fs.String("out", "", "directory to write converted migrations to")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *from == *to {
		return fmt.Errorf("-from and -to must be different formats")
	}
	if *in == "" || *out == "" {
		return fmt.Errorf("-in and -out are required")
	}

	migrations, err := readMigrations(*from, *in)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	for _, m := range migrations {
		if err := writeMigration(*to, *out, m); err != nil {
			return err
		}
	}

	fmt.Printf("converted %d migration(s) from %s to %s\n", len(migrations), *from, *to)
	return nil
}

func readMigrations(format, dir string) ([]migconv.Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	switch format {
	case "goose":
		return readGooseDir(dir, entries)
	case "golang-migrate":
		return readGolangMigrateDir(dir, entries)
	default:
		return nil, fmt.Errorf("unknown -from format %q", format)
	}
}

func readGooseDir(dir string, entries []os.DirEntry) ([]migconv.Migration, error) {
	var migrations []migconv.Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		m, err := migconv.ParseGoose(e.Name(), string(content))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, m)
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

func readGolangMigrateDir(dir string, entries []os.DirEntry) ([]migconv.Migration, error) {
	type pair struct {
		upFilename, downFilename string
	}
	pairs := map[string]*pair{}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		version, name, direction, err := migconv.ParseGolangMigrateFilename(e.Name())
		if err != nil {
			continue // skip files that aren't golang-migrate migrations
		}
		key := version + "_" + name
		p := pairs[key]
		if p == nil {
			p = &pair{}
			pairs[key] = p
		}
		if direction == "up" {
			p.upFilename = e.Name()
		} else {
			p.downFilename = e.Name()
		}
	}

	var keys []string
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var migrations []migconv.Migration
	for _, k := range keys {
		p := pairs[k]
		if p.upFilename == "" || p.downFilename == "" {
			return nil, fmt.Errorf("migration %q is missing its up or down file", k)
		}
		upContent, err := os.ReadFile(filepath.Join(dir, p.upFilename))
		if err != nil {
			return nil, err
		}
		downContent, err := os.ReadFile(filepath.Join(dir, p.downFilename))
		if err != nil {
			return nil, err
		}
		m, err := migconv.FromGolangMigrate(p.upFilename, string(upContent), p.downFilename, string(downContent))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, m)
	}
	return migrations, nil
}

func writeMigration(format, dir string, m migconv.Migration) error {
	switch format {
	case "goose":
		filename, content := migconv.FormatGoose(m)
		return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	case "golang-migrate":
		upFilename, upContent, downFilename, downContent := migconv.ToGolangMigrate(m)
		if err := os.WriteFile(filepath.Join(dir, upFilename), []byte(upContent), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, downFilename), []byte(downContent), 0o644)
	default:
		return fmt.Errorf("unknown -to format %q", format)
	}
}
