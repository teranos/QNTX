// Command parity prints, for every thing QNTX persists, whether SQLite and
// DuckDB each hold it.
//
// "can it be made to lie less ?"
//
// A thing is something QNTX has to keep. It exists independently of any
// backend: access tokens are a thing before either backend stores them, which
// is why NO/NO is a line and not an absence. That line is the point of the
// tool — it is how a human sees work that has not been done yet.
//
// Two sources, both code:
//
//   - Schema. Each backend's migrations are applied by its own runner, in the
//     engine the node links — ats-sqlite in SQLite, ats-duckdb in DuckDB — and
//     the resulting table list read back. Final state, not the CREATE
//     statements along the way, so a rebuild's scratch table is never mistaken
//     for a thing.
//
//   - Contracts. A Go interface declaring storage operations names a thing
//     whether or not anything implements it. TokenStore in server/auth is the
//     case that matters: the contract is written, no backend satisfies it.
//
// It prints the picture and writes what it read to server/parity/storage.json,
// which the node embeds and the parity sigil serves: "Make parity would just
// be for the storage backend specifically", and "parity the sigil is what an
// Agent should deal with through MCP".
//
// The output ranks nothing and scores nothing. No column is the baseline the
// others are measured against, and a line reads the same either way.
//
// No regex (see CLAUDE.md).
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/server/parity"
	errors "github.com/teranos/sacred-error"
)

// Thing is something QNTX persists, and where it is kept.
type Thing struct {
	Name   string
	SQLite bool
	DuckDB bool
	// Rebuilt rows cascade from attestations, so a take-in rebuilds them.
	Rebuilt bool
	// Sites are the places in Go that reach this thing with hand-written SQL.
	// They are why a column cannot change: SQL in a handler holds the SQLite
	// handle whatever the config says, so there is no seam for another backend
	// to satisfy.
	Sites []Site
}

func main() {
	root := flag.String("root", ".", "repository root to scan for storage contracts")
	crateDir := flag.String("crate", "crates/ats-duckdb/src", "DuckDB backend crate, scanned for object prefixes")
	flag.Parse()

	things, err := Report(*root, *crateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
		os.Exit(1)
	}
	body, err := Written(*root, things)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
		os.Exit(1)
	}
	path := filepath.Join(*root, parity.StorageFile)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", errors.Wrapf(err, "failed to write %s", path))
		os.Exit(1)
	}
	fmt.Print(Render(things))
}

// Written is the things as the node embeds them: each site by its file, once,
// and relative to root, so the file reads the same wherever it was run from.
func Written(root string, things []Thing) ([]byte, error) {
	stored := make([]parity.Stored, 0, len(things))
	for _, t := range things {
		files := []string{}
		for _, site := range t.Sites {
			file, err := filepath.Rel(root, site.File)
			if err != nil {
				return nil, errors.Wrapf(err, "%s is not under %s", site.File, root)
			}
			files = append(files, filepath.ToSlash(file))
		}
		sort.Strings(files)
		files = slices.Compact(files)
		stored = append(stored, parity.Stored{Name: t.Name, SQLite: t.SQLite, DuckDB: t.DuckDB, Rebuilt: t.Rebuilt, Sites: files})
	}
	body, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "the things did not marshal")
	}
	return append(body, '\n'), nil
}

// Report derives every thing and its presence in each backend.
func Report(root, crateDir string) ([]Thing, error) {
	sqliteTables, rebuilt, linked, err := SQLiteSchema()
	if err != nil {
		return nil, err
	}
	pins := filepath.Join(root, filepath.Dir(parity.StorageFile))
	pinned, err := parity.Pinned(pins, "sqlite")
	if err != nil {
		return nil, err
	}
	if linked != pinned {
		return nil, errors.Newf("the node links SQLite %s, %s pins %s", linked, pins, pinned)
	}
	duckdbTables, err := DuckDBSchema()
	if err != nil {
		return nil, err
	}
	// Most of what ats-duckdb keeps is objects under a prefix, not tables
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
	for name := range rebuilt {
		get(name).Rebuilt = true
	}
	for name := range duckdbTables {
		get(name).DuckDB = true
	}
	for name := range objectPrefixes {
		get(name).DuckDB = true
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

// replays names each node opened, since database/sql keeps a driver for the
// life of the process.
var replays atomic.Int64

// SQLiteSchema returns the tables SQLite ends up with, and the SQLite that
// answered, by opening a node's store and reading the schema back. The store
// is ats-sqlite's: its migrations, run by its runner, in the SQLite the node
// links. So the answer is not a reading of the migrations — it is the
// migrations' result on the node.
func SQLiteSchema() (_ map[string]bool, rebuilt map[string]bool, version string, err error) {
	// One directory for every store this process opens: ats-sqlite's flight
	// recorder writes beside the first store it is given, for the life of the
	// process, so that directory outlives each store.
	dir := filepath.Join(os.TempDir(), "qntx-parity")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, "", errors.Wrapf(err, "failed to make %s for the node's store", dir)
	}
	replay := replays.Add(1)
	path := filepath.Join(dir, fmt.Sprintf("node-%d-%d.db", os.Getpid(), replay))
	defer func() {
		for _, file := range []string{path, path + "-wal", path + "-shm"} {
			if rmErr := os.Remove(file); rmErr != nil && !os.IsNotExist(rmErr) && err == nil {
				err = errors.Wrapf(rmErr, "failed to remove %s", file)
			}
		}
	}()

	store, err := sqlitecgo.NewFileStore(path)
	if err != nil {
		return nil, nil, "", errors.Wrapf(err, "failed to open the node's store at %s", path)
	}
	defer func() { err = sqlclose.With(err, store.Close(), "the node's store at "+path) }()

	driver := fmt.Sprintf("rustsqlite-parity-%d", replay)
	rustdriver.RegisterNamed(driver, "parity", store.StorePtr(), store.ReadConnPtr(), store.Mu(), store.MuRead())
	db, err := sql.Open(driver, path)
	if err != nil {
		return nil, nil, "", errors.Wrapf(err, "failed to open %s through %s", path, driver)
	}
	defer func() { err = sqlclose.With(err, db.Close(), "the node's db at "+path) }()

	if err := db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		return nil, nil, "", errors.Wrapf(err, "failed to ask %s which SQLite it is", path)
	}
	tables, err := tableNames(db)
	if err != nil {
		return nil, nil, "", err
	}
	rebuilt, err = cascadesFrom(db, tables, "attestations")
	if err != nil {
		return nil, nil, "", err
	}
	return tables, rebuilt, version, nil
}

// cascadesFrom is every table whose rows are deleted with a row of parent,
// read from the schema's foreign keys rather than from a list.
func cascadesFrom(db *sql.DB, tables map[string]bool, parent string) (map[string]bool, error) {
	found := map[string]bool{}
	for name := range tables {
		cascades, err := cascadeOf(db, name, parent)
		if err != nil {
			return nil, err
		}
		if cascades {
			found[name] = true
		}
	}
	return found, nil
}

func cascadeOf(db *sql.DB, table, parent string) (_ bool, err error) {
	rows, err := db.Query(`SELECT "table", on_delete FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return false, errors.Wrapf(err, "failed to read the foreign keys of %s", table)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "the foreign keys of "+table) }()

	cascades := false
	for rows.Next() {
		var target, onDelete string
		if err := rows.Scan(&target, &onDelete); err != nil {
			return false, errors.Wrapf(err, "failed to scan a foreign key of %s", table)
		}
		if target == parent && onDelete == "CASCADE" {
			cascades = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, errors.Wrapf(err, "failed reading the foreign keys of %s", table)
	}
	return cascades, nil
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
func tableNames(db *sql.DB) (_ map[string]bool, err error) {
	rows, err := db.Query("SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read schema from sqlite_master")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for tableNames") }()

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

// Render draws the picture: one line per thing, a column per engine.
func Render(things []Thing) string {
	width := len("access_tokens")
	for _, t := range things {
		if len(t.Name) > width {
			width = len(t.Name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n  %-*s  SQLITE  DUCKDB\n", width, "")
	for _, t := range things {
		line := fmt.Sprintf("  %-*s  %-6s  %-6s", width, t.Name, mark(t.SQLite), mark(t.DuckDB))
		if t.Rebuilt {
			line += "  rebuilt from attestations"
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
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
