// Package store is Heliostat's durable job index in SQLite. Every RayJob, Jobs API submission, and
// driver Heliostat observes is recorded here, so the UI and API never query clusters per request.
//
// The schema and JSON payloads are compatible with earlier Heliostat releases.
package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver; no cgo, so the binary stays static.

	"github.com/KubedAI/heliostat/internal/domain"
)

var windows = map[domain.TimeWindow]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// migrations run in order; PRAGMA user_version records how many have been applied.
var migrations = []func(tx *sql.Tx) error{
	func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE jobs (
				id          TEXT PRIMARY KEY,
				cluster     TEXT NOT NULL,
				namespace   TEXT NOT NULL,
				kind        TEXT NOT NULL,
				status      TEXT NOT NULL,
				terminal    INTEGER NOT NULL,
				present     INTEGER NOT NULL,
				ray_cluster TEXT,
				created_ms  INTEGER NOT NULL,
				settled_ms  INTEGER,
				search      TEXT NOT NULL,
				payload     TEXT NOT NULL
			) STRICT;
			CREATE INDEX jobs_created ON jobs (created_ms DESC, id DESC);
			CREATE INDEX jobs_ray_cluster ON jobs (ray_cluster);
			CREATE INDEX jobs_retention ON jobs (present, settled_ms);`)
		return err
	},
}

// Page is one page of a job query.
type Page struct {
	Items        []domain.JobRecord
	NextCursor   string
	StatusCounts map[domain.JobStatus]int
	Namespaces   []string
}

// CloseAs describes how an unfinished job is closed when its backing object disappears.
type CloseAs struct {
	Status  domain.JobStatus
	Reason  string
	Message string
}

// Store is safe for concurrent use.
type Store struct {
	db       *sql.DB
	mu       sync.Mutex // serializes read-modify-write upserts
	revision atomic.Int64
}

// Open opens (creating if needed) the database at path. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dsn = "file:" + path
	}
	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite serializes writes anyway, and ":memory:" is per-connection.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Revision increments on every write; used for HTTP ETags.
func (s *Store) Revision() int64 { return s.revision.Load() }

// Ping checks the database is usable.
func (s *Store) Ping() error {
	var n int
	return s.db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&n)
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err := migrations[i](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Get returns a job by ID.
func (s *Store) Get(id string) (domain.JobRecord, bool) {
	var payload string
	if err := s.db.QueryRow("SELECT payload FROM jobs WHERE id = ?", id).Scan(&payload); err != nil {
		return domain.JobRecord{}, false
	}
	var job domain.JobRecord
	if json.Unmarshal([]byte(payload), &job) != nil {
		return domain.JobRecord{}, false
	}
	return job, true
}

// Upsert inserts or updates a record and reports whether anything changed. Fields only another
// source knows (Ray's hex job ID, learned from the Jobs API for a RayJob CR) are carried over, and
// submissions keep their first-seen creation time.
func (s *Store) Upsert(job domain.JobRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, found := s.Get(job.ID)
	if found {
		if job.RayJobID == "" {
			job.RayJobID = existing.RayJobID
		}
		if job.RayClusterUID == "" {
			job.RayClusterUID = existing.RayClusterUID
		}
		if job.Kind != domain.KindRayJob {
			job.CreatedAt = existing.CreatedAt
		}
		if sameJSON(existing, job) {
			return false, nil
		}
	}
	if err := writeRow(s.db, job, 0); err != nil {
		return false, err
	}
	s.revision.Add(1)
	return true, nil
}

// MarkGone records that a job's backing object no longer exists. Unfinished jobs are closed.
func (s *Store) MarkGone(id string, closeAs CloseAs, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, found := s.Get(id)
	if !found || !job.Present {
		return false, nil
	}
	job.Present = false
	if !job.Status.Terminal() {
		job.Status, job.Reason, job.Message, job.Phase = closeAs.Status, closeAs.Reason, closeAs.Message, closeAs.Reason
		job.FinishedAt = isoMillis(now)
	}
	if err := writeRow(s.db, job, now.UnixMilli()); err != nil {
		return false, err
	}
	s.revision.Add(1)
	return true, nil
}

// PresentIDs lists jobs of the given kinds in cluster that Heliostat believes still exist.
func (s *Store) PresentIDs(cluster string, kinds ...domain.JobKind) ([]string, error) {
	args := []any{cluster}
	for _, k := range kinds {
		args = append(args, string(k))
	}
	rows, err := s.db.Query(fmt.Sprintf(
		"SELECT id FROM jobs WHERE cluster = ? AND present = 1 AND kind IN (%s)", placeholders(len(kinds))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PresentSubmissionsByRayCluster maps "<namespace>/<ray cluster>" to present submission and driver IDs.
func (s *Store) PresentSubmissionsByRayCluster(cluster string) (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT id, ray_cluster FROM jobs
		WHERE cluster = ? AND present = 1 AND kind IN ('Submission', 'Driver')`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id string
		var rc sql.NullString
		if err := rows.Scan(&id, &rc); err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(rc.String, cluster+"/")
		out[key] = append(out[key], id)
	}
	return out, rows.Err()
}

// ActiveJobsByRayCluster counts unfinished present jobs per "<cluster>/<namespace>/<ray cluster>".
func (s *Store) ActiveJobsByRayCluster() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT ray_cluster, COUNT(*) FROM jobs
		WHERE present = 1 AND terminal = 0 AND ray_cluster IS NOT NULL GROUP BY ray_cluster`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

// Counts returns the total and active job counts.
func (s *Store) Counts() (jobs, active int, err error) {
	var a sql.NullInt64
	err = s.db.QueryRow("SELECT COUNT(*), SUM(present = 1 AND terminal = 0) FROM jobs").Scan(&jobs, &a)
	return jobs, int(a.Int64), err
}

// Query filters, facets, and pages jobs, newest first. Unfinished jobs stay visible whatever their age.
func (s *Store) Query(q domain.JobQuery, now time.Time) (Page, error) {
	var where []string
	var args []any
	if d, ok := windows[q.Window]; ok {
		where = append(where, "(created_ms >= ? OR terminal = 0)")
		args = append(args, now.Add(-d).UnixMilli())
	}
	if q.Cluster != "" {
		where, args = append(where, "cluster = ?"), append(args, q.Cluster)
	}
	scopeWhere, scopeArgs := clone(where), clone(args)
	if q.Namespace != "" {
		where, args = append(where, "namespace = ?"), append(args, q.Namespace)
	}
	if q.Kind != "" {
		where, args = append(where, "kind = ?"), append(args, string(q.Kind))
	}
	if q.Q != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q.Q))
		where, args = append(where, `search LIKE ? ESCAPE '\'`), append(args, "%"+escaped+"%")
	}
	facetWhere, facetArgs := clone(where), clone(args)
	if q.Status != "" {
		where, args = append(where, "status = ?"), append(args, string(q.Status))
	}
	if ms, id, ok := decodeCursor(q.Cursor); ok {
		where, args = append(where, "(created_ms, id) < (?, ?)"), append(args, ms, id)
	}

	page := Page{StatusCounts: map[domain.JobStatus]int{}, Items: []domain.JobRecord{}, Namespaces: []string{}}
	rows, err := s.db.Query(fmt.Sprintf("SELECT payload, created_ms, id FROM jobs %s ORDER BY created_ms DESC, id DESC LIMIT ?",
		clause(where)), append(args, q.Limit+1)...)
	if err != nil {
		return page, err
	}
	var lastMs int64
	var lastID string
	for rows.Next() {
		var payload, id string
		var ms int64
		if err := rows.Scan(&payload, &ms, &id); err != nil {
			rows.Close()
			return page, err
		}
		if len(page.Items) == q.Limit {
			page.NextCursor = encodeCursor(lastMs, lastID)
			break
		}
		var job domain.JobRecord
		if err := json.Unmarshal([]byte(payload), &job); err == nil {
			page.Items = append(page.Items, job)
		}
		lastMs, lastID = ms, id
	}
	rows.Close()

	facetRows, err := s.db.Query(fmt.Sprintf("SELECT status, COUNT(*) FROM jobs %s GROUP BY status", clause(facetWhere)), facetArgs...)
	if err != nil {
		return page, err
	}
	for facetRows.Next() {
		var status string
		var n int
		if err := facetRows.Scan(&status, &n); err == nil {
			page.StatusCounts[domain.JobStatus(status)] = n
		}
	}
	facetRows.Close()

	nsRows, err := s.db.Query(fmt.Sprintf("SELECT DISTINCT namespace FROM jobs %s ORDER BY namespace", clause(scopeWhere)), scopeArgs...)
	if err != nil {
		return page, err
	}
	defer nsRows.Close()
	for nsRows.Next() {
		var ns string
		if err := nsRows.Scan(&ns); err == nil {
			page.Namespaces = append(page.Namespaces, ns)
		}
	}
	return page, nsRows.Err()
}

// Purge deletes jobs whose backing object has been gone for longer than days.
func (s *Store) Purge(days int, now time.Time) (int64, error) {
	res, err := s.db.Exec("DELETE FROM jobs WHERE present = 0 AND settled_ms < ?", now.Add(-time.Duration(days)*24*time.Hour).UnixMilli())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.revision.Add(1)
	}
	return n, nil
}

// RayClusterKey is the ray_cluster column value.
func RayClusterKey(cluster, namespace, name string) string {
	return cluster + "/" + namespace + "/" + name
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func writeRow(db execer, job domain.JobRecord, settledMs int64) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	var rayCluster any
	if job.RayClusterName != "" {
		rayCluster = RayClusterKey(job.Cluster, job.Namespace, job.RayClusterName)
	}
	createdMs := parseMs(job.CreatedAt)
	if createdMs == 0 {
		createdMs = time.Now().UnixMilli()
	}
	var settled any
	switch {
	case parseMs(job.FinishedAt) != 0:
		settled = parseMs(job.FinishedAt)
	case settledMs != 0:
		settled = settledMs
	case !job.Present:
		settled = time.Now().UnixMilli()
	}
	_, err = db.Exec(`
		INSERT INTO jobs (id, cluster, namespace, kind, status, terminal, present, ray_cluster,
		                  created_ms, settled_ms, search, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			status = excluded.status, terminal = excluded.terminal, present = excluded.present,
			ray_cluster = excluded.ray_cluster, settled_ms = excluded.settled_ms,
			search = excluded.search, payload = excluded.payload`,
		job.ID, job.Cluster, job.Namespace, string(job.Kind), string(job.Status), boolInt(job.Status.Terminal()),
		boolInt(job.Present), rayCluster, createdMs, settled, searchText(job), string(payload))
	return err
}

func searchText(job domain.JobRecord) string {
	var parts []string
	for _, p := range []string{job.Name, job.Namespace, job.Cluster, job.Model, job.Owner, job.WorkloadType,
		job.RayClusterName, job.SubmissionID, string(job.Kind)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func sameJSON(a, b domain.JobRecord) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func parseMs(iso string) int64 {
	if iso == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func isoMillis(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

func clause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func clone[T any](s []T) []T { return append([]T(nil), s...) }

func encodeCursor(ms int64, id string) string {
	data, _ := json.Marshal([]any{ms, id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(cursor string) (int64, string, bool) {
	if cursor == "" {
		return 0, "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", false
	}
	var parts []any
	if json.Unmarshal(data, &parts) != nil || len(parts) != 2 {
		return 0, "", false
	}
	ms, ok1 := parts[0].(float64)
	id, ok2 := parts[1].(string)
	return int64(ms), id, ok1 && ok2
}
