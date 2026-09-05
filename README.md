# sql-migrate-convert

Converts SQL migration files between the two layouts I keep running
into:

- **golang-migrate**: each migration is a pair of files,
  `NNN_name.up.sql` and `NNN_name.down.sql`.
- **goose**: each migration is a single file, `NNN_name.sql`, with
  `-- +goose Up` and `-- +goose Down` markers inside it.

Switching migration tools, or just trying one out on an existing
project, means rewriting every migration file by hand. This does that
rewrite for you.

## Library

The conversion logic lives in `migconv` and has no dependency on the
filesystem. Every exported function takes strings in and returns
strings (or a `Migration` value) out, so it's straightforward to unit
test and to embed in something other than the CLI.

```go
m, err := migconv.FromGolangMigrate(
    "000001_create_users.up.sql", "CREATE TABLE users (id INTEGER);\n",
    "000001_create_users.down.sql", "DROP TABLE users;\n",
)
if err != nil {
    log.Fatal(err)
}

filename, content := migconv.FormatGoose(m)
// filename == "000001_create_users.sql"
// content  == "-- +goose Up\nCREATE TABLE users (id INTEGER);\n\n-- +goose Down\nDROP TABLE users;\n"
```

The reverse direction works the same way: `migconv.ParseGoose` reads a
goose file into a `Migration`, and `migconv.ToGolangMigrate` renders it
back out as an up/down pair.

## CLI

```
go run ./cmd/migconv -from golang-migrate -to goose -in ./migrations -out ./goose_migrations
go run ./cmd/migconv -from goose -to golang-migrate -in ./goose_migrations -out ./migrations
```

`-in` is scanned non-recursively; unrelated files are ignored. `-out`
is created if it doesn't exist. Existing files in `-out` with the same
name are overwritten.

## Known limitations (first pass)

- Goose's `-- +goose StatementBegin` / `-- +goose StatementEnd`
  markers, used to protect multi-statement bodies like triggers, are
  passed through as plain text rather than understood structurally.
- Only the sequential/timestamp-prefixed filename convention is
  supported; goose's `.env` variant is out of scope for now.

## License

MIT, see [LICENSE](LICENSE).
