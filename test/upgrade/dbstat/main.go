// SPDX-License-Identifier: AGPL-3.0-or-later

// Command dbstat reads the panel database for the upgrade test
// (test/upgrade/run.sh) while the panel is stopped. It runs in a container
// with the data volume, so it needs no sqlite3 on the host.
//
//	dbstat counts DB             rows per table, tab separated
//	dbstat query DB SQL          rows of a query, tab separated
//	dbstat exec DB SQL           a writing statement (test data only)
//	dbstat ls DIR                file names in DIR
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 3 {
		fail(fmt.Errorf("usage: dbstat counts|query|exec|ls PATH [SQL]"))
	}
	fail(run(context.Background(), os.Args[1], os.Args[2]))
}

func run(ctx context.Context, cmd, path string) (err error) {
	if cmd == "ls" {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Println(e.Name())
		}
		return nil
	}
	mode := "ro"
	if cmd == "exec" {
		mode = "rw"
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode="+mode+"&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	switch cmd {
	case "counts":
		return counts(ctx, db)
	case "query":
		return query(ctx, db, arg(3))
	case "exec":
		res, err := db.ExecContext(ctx, arg(3))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		fmt.Println("rows affected:", n)
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func arg(i int) string {
	if len(os.Args) <= i {
		fail(fmt.Errorf("missing argument %d", i))
	}
	return os.Args[i]
}

func tables(ctx context.Context, db *sql.DB) (names []string, err error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func counts(ctx context.Context, db *sql.DB) error {
	names, err := tables(ctx, db)
	if err != nil {
		return err
	}
	for _, n := range names {
		var c int
		// The name comes from sqlite_master and is quoted as an identifier.
		q := `SELECT COUNT(*) FROM "` + strings.ReplaceAll(n, `"`, `""`) + `"`
		if err := db.QueryRowContext(ctx, q).Scan(&c); err != nil { // #nosec G701 -- table name from sqlite_master, quoted
			return err
		}
		fmt.Printf("%s\t%d\n", n, c)
	}
	return nil
}

func query(ctx context.Context, db *sql.DB, q string) (err error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = v.String
			if !v.Valid {
				out[i] = "NULL"
			}
		}
		fmt.Println(strings.Join(out, "\t"))
	}
	return rows.Err()
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbstat:", err)
		os.Exit(1)
	}
}
