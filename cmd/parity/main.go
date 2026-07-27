// Command parity prints, for every thing QNTX persists, whether each storage
// backend holds it.
//
// A thing is something QNTX has to keep. It exists independently of any
// backend: access tokens are a thing before either backend stores them, which
// is why NO/NO is a line and not an absence. That line is the point of the
// tool — it is how a human sees work that has not been done yet.
//
// Two sources, both code:
//
//   - Schema. Migrations are replayed to their final state and the resulting
//     table list read back. Final state, not the CREATE statements along the
//     way, so a rebuild's scratch table is never mistaken for a thing.
//
//   - Contracts. A Go interface declaring storage operations names a thing
//     whether or not anything implements it. TokenStore in server/auth is the
//     case that matters: the contract is written, no backend satisfies it.
//
// The output ranks nothing and scores nothing. Neither backend is the baseline
// the other is measured against — parquet is the reference implementation for
// some things, SQLite for others, and a line reads the same either way.
//
// No regex (see CLAUDE.md).
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	qntxdb "github.com/teranos/QNTX/db"
	"github.com/teranos/errors"
)

func init() {
	// Migrations that build vector indexes need the vec0 module registered
	// before any connection opens, same as production.
	sqlitevec.Auto()
}

// Thing is something QNTX persists, and which backends hold it.
type Thing struct {
	Name    string
	SQLite  bool
	Parquet bool
	// Sites are the places in Go that reach this thing with hand-written SQL.
	// They are why a column cannot change: SQL in a handler holds the SQLite
	// handle whatever the config says, so there is no seam for another backend
	// to satisfy.
	Sites []Site
}

func main() {
	root := flag.String("root", ".", "repository root to scan for storage contracts")
	parquetDir := flag.String("parquet", "db/duckdb/migrations", "DuckDB/parquet migrations directory")
	crateDir := flag.String("crate", "crates/qntx-duckdb/src", "DuckDB backend crate, scanned for object prefixes")
	flag.Parse()

	things, err := Report(*root, *parquetDir, *crateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(Render(things))
}

// Report derives every thing and its presence in each backend.
func Report(root, parquetDir, crateDir string) ([]Thing, error) {
	sqliteTables, err := SQLiteSchema()
	if err != nil {
		return nil, err
	}
	parquetTables, err := ReplaySchema(filepath.Join(root, parquetDir))
	if err != nil {
		return nil, err
	}
	// Most of the parquet backend is objects under a prefix, not tables
	// (ADR-024:40-45). Without these the column could only ever describe
	// attestations and the append-only logs.
	objectPrefixes, err := ObjectPrefixes(filepath.Join(root, crateDir))
	if err != nil {
		return nil, err
	}

	present := map[string]*Thing{}
	get := func(name string) *Thing {
		if t, ok := present[name]; ok {
			return t
		}
		t := &Thing{Name: name}
		present[name] = t
		return t
	}
	for name := range sqliteTables {
		get(name).SQLite = true
	}
	for name := range parquetTables {
		get(name).Parquet = true
	}
	for name := range objectPrefixes {
		get(name).Parquet = true
	}

	// Contracts add the things no backend holds yet. A contract whose name
	// already matches schema is the same thing, not a second one.
	unimplemented, err := UnimplementedContracts(root)
	if err != nil {
		return nil, err
	}
	for _, name := range unimplemented {
		if covered(name, present) {
			continue
		}
		get(name)
	}

	known := make(map[string]bool, len(present))
	for name := range present {
		known[name] = true
	}
	sites, err := StatementSites(root, known)
	if err != nil {
		return nil, err
	}
	for name, found := range sites {
		present[name].Sites = found
	}

	things := make([]Thing, 0, len(present))
	for _, t := range present {
		things = append(things, *t)
	}
	sort.Slice(things, func(i, j int) bool { return things[i].Name < things[j].Name })
	return things, nil
}

// covered reports whether storage already names this thing under a longer
// name. A contract yields a name from its type — TokenStore gives "tokens" —
// while storage names the same thing "access_tokens". Without this the picture
// carries both, one of them permanently NO/NO, describing work that is already
// done under the other line.
func covered(contract string, present map[string]*Thing) bool {
	for name := range present {
		if name != contract && strings.Contains(name, contract) {
			return true
		}
	}
	return false
}

// SQLiteSchema returns the tables SQLite ends up with, by running the real
// migration runner against an in-memory database and reading the schema back.
// This is the same code path production takes, so the answer is not a reading
// of the migrations — it is the migrations' result.
func SQLiteSchema() (map[string]bool, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, errors.Wrap(err, "failed to open in-memory SQLite for schema replay")
	}
	defer db.Close()
	// Each :memory: connection is its own database; a second pooled connection
	// would see an empty schema.
	db.SetMaxOpenConns(1)

	if err := qntxdb.Migrate(db, nil); err != nil {
		return nil, errors.Wrap(err, "failed to replay SQLite migrations")
	}
	return tableNames(db)
}

// ReplaySchema runs the .sql files in dir, in filename order, against an
// in-memory database and returns the tables left standing.
//
// DuckDB's migration runner lives in Rust (crates/qntx-duckdb/src/migrate.rs),
// so its migrations are replayed here rather than executed by their own engine.
// The DDL is portable enough for SQLite to accept; anything it rejects fails
// this command loudly instead of being guessed at.
func ReplaySchema(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read migrations from %s", dir)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open in-memory SQLite replaying %s", dir)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	for _, name := range files {
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read migration %s", path)
		}
		if _, err := db.Exec(string(body)); err != nil {
			// Matches the production runner: migrations named "optional"
			// depend on extensions that may not be loaded (db/migrate.go).
			if strings.Contains(name, "optional") {
				continue
			}
			return nil, errors.Wrapf(err, "failed to execute migration %s", path)
		}
	}
	return tableNames(db)
}

// tableNames reads the tables a database ended up with, minus the ones that
// are not things QNTX persists:
//
//   - schema_migrations, the runner's own bookkeeping, present in every
//     backend by construction.
//   - shadow tables. A virtual table such as vec_embeddings materialises
//     vec_embeddings_chunks, _rowids and friends; they are that index's
//     internals, and listing them would put five lines on the picture where
//     the developer created one.
func tableNames(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read schema from sqlite_master")
	}
	defer rows.Close()

	var all []string
	var virtual []string
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return nil, errors.Wrap(err, "failed to scan table name from sqlite_master")
		}
		if name == "schema_migrations" || strings.HasPrefix(name, "sqlite_") {
			continue
		}
		all = append(all, name)
		if strings.Contains(strings.ToUpper(ddl), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed reading sqlite_master rows")
	}

	names := map[string]bool{}
	for _, name := range all {
		if shadowOf(name, virtual) {
			continue
		}
		names[name] = true
	}
	return names, nil
}

// shadowOf reports whether name is storage belonging to one of the virtual
// tables rather than a thing in its own right.
func shadowOf(name string, virtual []string) bool {
	for _, v := range virtual {
		if name != v && strings.HasPrefix(name, v+"_") {
			return true
		}
	}
	return false
}

// Render draws the picture: one line per thing, one column per backend.
func Render(things []Thing) string {
	width := len("access_tokens")
	for _, t := range things {
		if len(t.Name) > width {
			width = len(t.Name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s  SQLITE | PARQUET\n", strings.Repeat(" ", width))
	for _, t := range things {
		fmt.Fprintf(&b, "  %-*s  %-6s   %s\n", width, t.Name, mark(t.SQLite), mark(t.Parquet))
		for _, s := range t.Sites {
			fmt.Fprintf(&b, "      %s:%d\n", s.File, s.Line)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func mark(present bool) string {
	if present {
		return "YES"
	}
	return "NO"
}
