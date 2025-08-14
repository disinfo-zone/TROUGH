package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type backupPayload struct {
	FormatVersion int                        `json:"format_version"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	Tables        map[string]json.RawMessage `json:"tables"`
	Notes         string                     `json:"notes,omitempty"`
}

// includedTables returns the set of logical tables we back up and restore.
func includedTables() []string {
	// Order matters for restore, but we store as a map; we keep a separate ordered slice for restore
	return []string{
		"site_settings",
		"users",
		"pages",
		"images",
		"likes",
		"collections",
		"invites",
		"cms_tombstones",
		"password_resets",
		"email_verifications",
	}
}

// DumpTableJSON returns the JSON array of rows for a given table using Postgres row_to_json.
func DumpTableJSON(ctx context.Context, db *sqlx.DB, table string) (json.RawMessage, error) {
	// We wrap with COALESCE to ensure we always get [] when empty
	q := fmt.Sprintf("SELECT COALESCE(json_agg(t), '[]'::json) FROM (SELECT * FROM %s) t", table)
	var data json.RawMessage
	if err := db.QueryRowxContext(ctx, q).Scan(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// CreateBackup builds a gzipped JSON backup of the database contents for selected tables.
func CreateBackup(ctx context.Context, db *sqlx.DB) ([]byte, string, error) {
	payload := backupPayload{
		FormatVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		Tables:        make(map[string]json.RawMessage, 8),
		Notes:         "Application data only; no binary uploads included.",
	}
	tables := includedTables()
	for _, t := range tables {
		data, err := DumpTableJSON(ctx, db, t)
		if err != nil {
			return nil, "", err
		}
		payload.Tables[t] = data
	}
	js, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, "", err
	}
	// gzip without extra string copy
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write(js); err != nil {
		_ = gz.Close()
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	name := "trough-backup-" + payload.GeneratedAt.Format("20060102T150405Z") + ".json.gz"
	return b.Bytes(), name, nil
}

// RestoreBackup consumes a backup stream (gzipped JSON or raw JSON) and restores tables in a transaction.
// This replaces existing data in the included tables. It does not touch binary uploads.
func RestoreBackup(ctx context.Context, db *sqlx.DB, r io.Reader) error {
	// Attempt to read gzip; if it fails, fall back to plain JSON
	var dec io.Reader
	if zr, err := gzip.NewReader(r); err == nil {
		defer zr.Close()
		dec = zr
	} else {
		dec = r
	}
	var payload backupPayload
	if err := json.NewDecoder(dec).Decode(&payload); err != nil {
		return err
	}
	// Basic format check
	if payload.FormatVersion <= 0 {
		return fmt.Errorf("invalid backup format")
	}
	// Start transaction
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Disable triggers and defer constraints within transaction (best-effort)
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		// continue anyway
		_ = err
	}

	// Truncate in reverse dependency order: children first
	truncateOrder := []string{"likes", "collections", "images", "invites", "pages", "cms_tombstones", "users", "site_settings"}
	for _, t := range truncateOrder {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", t)); err != nil {
			return err
		}
	}
	// Insert in dependency order
	insertOrder := includedTables()
	for _, t := range insertOrder {
		data, ok := payload.Tables[t]
		if !ok || len(data) == 0 {
			continue
		}
		// Skip empty arrays "[]"
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "[]" || trimmed == "null" || trimmed == "" {
			continue
		}
		// Determine which columns are present in backup to let DB defaults apply for new columns
		cols, err := unionJSONKeys(data)
		if err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
		if len(cols) == 0 {
			continue
		}
		validCols, err := getTableColumns(ctx, tx, t)
		if err != nil {
			return fmt.Errorf("describe %s: %w", t, err)
		}
		colSet := make(map[string]bool, len(cols))
		for _, c := range cols {
			colSet[strings.ToLower(c)] = true
		}
		var finalCols []string
		for _, vc := range validCols {
			if colSet[strings.ToLower(vc)] {
				finalCols = append(finalCols, vc)
			}
		}
		if len(finalCols) == 0 {
			continue
		}
		// Build INSERT INTO table (c1,c2,...) SELECT t.c1,t.c2,... FROM json_populate_recordset(NULL::public.table,$1::json) AS t
		sel := make([]string, 0, len(finalCols))
		for _, c := range finalCols {
			sel = append(sel, "t."+pqQuoteIdent(c))
		}
		colList := pqQuoteIdents(finalCols, ",")
		selList := strings.Join(sel, ",")
		q := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM json_populate_recordset(NULL::%s.%s, $1::json) AS t", pqQuoteIdent(t), colList, selList, pqQuoteIdent("public"), pqQuoteIdent(t))
		if _, err := tx.ExecContext(ctx, q, data); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}

	return tx.Commit()
}

// getTableColumns returns column names for a table in ordinal order.
func getTableColumns(ctx context.Context, q sqlx.QueryerContext, table string) ([]string, error) {
	rows, err := q.QueryxContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// unionJSONKeys computes the union of keys present across an array of JSON objects.
func unionJSONKeys(data json.RawMessage) ([]string, error) {
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, m := range arr {
		for k := range m {
			set[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func pqQuoteIdent(ident string) string {
	// naive quote; replace double quotes with two quotes
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func pqQuoteIdents(ids []string, sep string) string {
	out := make([]string, 0, len(ids))
	for _, s := range ids {
		out = append(out, pqQuoteIdent(s))
	}
	return strings.Join(out, sep)
}

// SaveBackupFile writes a backup to the given directory and returns the absolute file path.
func SaveBackupFile(ctx context.Context, db *sqlx.DB, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "backups"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, name, err := CreateBackup(ctx, db)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

type BackupFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ListBackups returns metadata for backup files in dir.
func ListBackups(dir string) ([]BackupFile, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "backups"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupFile{}, nil
		}
		return nil, err
	}
	var out []BackupFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: name, Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// DeleteBackup removes a named backup file from dir.
func DeleteBackup(dir, name string) error {
	if strings.TrimSpace(dir) == "" {
		dir = "backups"
	}
	// sanitize name: must not contain separators
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.TrimSpace(name) == "" {
		return fmt.Errorf("invalid name")
	}
	return os.Remove(filepath.Join(dir, name))
}

// CleanupBackups deletes backup files older than keepDays.
func CleanupBackups(dir string, keepDays int) error {
	if keepDays <= 0 {
		return nil
	}
	list, err := ListBackups(dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	for _, f := range list {
		if f.ModTime.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, f.Name))
		}
	}
	return nil
}
