// Command migconv converts a directory of SQL migrations between the
// golang-migrate layout and the goose layout using the migconv package.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/patricia556/sql-migrate-convert/migconv"
)

// located pairs a Migration with the directory it was read from (relative
// to -in) or should be written to (relative to -out), so that migrations
// nested in subdirectories land back in the same subdirectory rather than
// all being flattened into -out itself.
type located struct {
	dir string
	m   migconv.Migration
}

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

	for _, loc := range migrations {
		if err := os.MkdirAll(filepath.Join(*out, loc.dir), 0o755); err != nil {
			return err
		}
		if err := writeMigration(*to, *out, loc); err != nil {
			return err
		}
	}

	fmt.Printf("converted %d migration(s) from %s to %s\n", len(migrations), *from, *to)
	return nil
}

// filesByDir walks root recursively and groups the regular files it finds
// by the directory (relative to root) they live in, so migrations nested
// in subdirectories are converted and written back into the same
// subdirectory instead of being flattened into -out.
func filesByDir(root string) (map[string][]string, error) {
	byDir := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		if relDir == "." {
			relDir = ""
		}
		byDir[relDir] = append(byDir[relDir], filepath.Base(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return byDir, nil
}

func readMigrations(format, root string) ([]located, error) {
	byDir, err := filesByDir(root)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var migrations []located
	for _, d := range dirs {
		names := byDir[d]
		sort.Strings(names)

		var found []located
		var err error
		switch format {
		case "goose":
			found, err = readGooseDir(filepath.Join(root, d), d, names)
		case "golang-migrate":
			found, err = readGolangMigrateDir(filepath.Join(root, d), d, names)
		default:
			return nil, fmt.Errorf("unknown -from format %q", format)
		}
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, found...)
	}
	return migrations, nil
}

func readGooseDir(dir, relDir string, names []string) ([]located, error) {
	var migrations []located
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		m, err := migconv.ParseGoose(name, string(content))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, located{dir: relDir, m: m})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].m.Version < migrations[j].m.Version })
	return migrations, nil
}

func readGolangMigrateDir(dir, relDir string, names []string) ([]located, error) {
	type pair struct {
		upFilename, downFilename string
	}
	pairs := map[string]*pair{}

	for _, name := range names {
		version, migName, direction, err := migconv.ParseGolangMigrateFilename(name)
		if err != nil {
			continue // skip files that aren't golang-migrate migrations
		}
		key := version + "_" + migName
		p := pairs[key]
		if p == nil {
			p = &pair{}
			pairs[key] = p
		}
		if direction == "up" {
			p.upFilename = name
		} else {
			p.downFilename = name
		}
	}

	var keys []string
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var migrations []located
	for _, k := range keys {
		p := pairs[k]
		if p.upFilename == "" || p.downFilename == "" {
			return nil, fmt.Errorf("migration %q is missing its up or down file", filepath.Join(relDir, k))
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
		migrations = append(migrations, located{dir: relDir, m: m})
	}
	return migrations, nil
}

func writeMigration(format, outRoot string, loc located) error {
	dir := filepath.Join(outRoot, loc.dir)
	switch format {
	case "goose":
		filename, content := migconv.FormatGoose(loc.m)
		return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	case "golang-migrate":
		upFilename, upContent, downFilename, downContent := migconv.ToGolangMigrate(loc.m)
		if err := os.WriteFile(filepath.Join(dir, upFilename), []byte(upContent), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, downFilename), []byte(downContent), 0o644)
	default:
		return fmt.Errorf("unknown -to format %q", format)
	}
}
