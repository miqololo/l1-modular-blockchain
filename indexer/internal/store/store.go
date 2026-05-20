// Package store is the SQLite-backed read model for the chain's marketplace
// state. Idempotent ingest via INSERT ... ON CONFLICT.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"

	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	logger *zap.Logger
	mu     sync.Mutex // SQLite serializes writers; this is belt+suspenders for our path
}

func Open(path string, logger *zap.Logger) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: single writer
	return &Store{db: db, logger: logger}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS services (
  id INTEGER PRIMARY KEY,
  owner TEXT NOT NULL,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  price_denom TEXT NOT NULL,
  price_amount INTEGER NOT NULL,
  verification_domain_id INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  created_at_height INTEGER NOT NULL,
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY,
  service_id INTEGER NOT NULL REFERENCES services(id),
  requester TEXT NOT NULL,
  input_hash TEXT NOT NULL,
  input_uri TEXT NOT NULL,
  input_text TEXT,
  escrow_denom TEXT NOT NULL,
  escrow_amount INTEGER NOT NULL,
  deadline_height INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'PENDING',
  created_at_height INTEGER NOT NULL,
  finalized_at_height INTEGER,
  output_hash TEXT,
  output_uri TEXT,
  output_text TEXT,
  provider TEXT,
  paid_denom TEXT,
  paid_amount INTEGER,
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX IF NOT EXISTS requests_service_idx ON requests(service_id);
CREATE INDEX IF NOT EXISTS requests_status_idx  ON requests(status);
CREATE INDEX IF NOT EXISTS requests_requester_idx ON requests(requester);

CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
`

func (s *Store) Migrate() error {
	_, err := s.db.Exec(schemaSQL)
	return err
}

// ─── Service ────────────────────────────────────────────────────────────────

type Service struct {
	ID                   uint64 `json:"id"`
	Owner                string `json:"owner"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	PriceDenom           string `json:"price_denom"`
	PriceAmount          uint64 `json:"price_amount"`
	VerificationDomainID uint64 `json:"verification_domain_id"`
	Active               bool   `json:"active"`
	CreatedAtHeight      int64  `json:"created_at_height"`
}

func (s *Store) UpsertService(svc Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO services (id, owner, name, description, price_denom, price_amount, verification_domain_id, active, created_at_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			owner=excluded.owner,
			description=excluded.description,
			price_denom=excluded.price_denom,
			price_amount=excluded.price_amount,
			active=excluded.active,
			updated_at=strftime('%s','now')
	`, svc.ID, svc.Owner, svc.Name, svc.Description, svc.PriceDenom, svc.PriceAmount, svc.VerificationDomainID, svc.Active, svc.CreatedAtHeight)
	return err
}

func (s *Store) GetService(id uint64) (Service, error) {
	row := s.db.QueryRow(`SELECT id, owner, name, description, price_denom, price_amount, verification_domain_id, active, created_at_height FROM services WHERE id = ?`, id)
	var sv Service
	if err := row.Scan(&sv.ID, &sv.Owner, &sv.Name, &sv.Description, &sv.PriceDenom, &sv.PriceAmount, &sv.VerificationDomainID, &sv.Active, &sv.CreatedAtHeight); err != nil {
		if err == sql.ErrNoRows {
			return Service{}, ErrNotFound
		}
		return Service{}, err
	}
	return sv, nil
}

func (s *Store) ListServices() ([]Service, error) {
	rows, err := s.db.Query(`SELECT id, owner, name, description, price_denom, price_amount, verification_domain_id, active, created_at_height FROM services ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Service{}
	for rows.Next() {
		var sv Service
		if err := rows.Scan(&sv.ID, &sv.Owner, &sv.Name, &sv.Description, &sv.PriceDenom, &sv.PriceAmount, &sv.VerificationDomainID, &sv.Active, &sv.CreatedAtHeight); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

// ─── Request ────────────────────────────────────────────────────────────────

type Request struct {
	ID                uint64  `json:"id"`
	ServiceID         uint64  `json:"service_id"`
	Requester         string  `json:"requester"`
	InputHash         string  `json:"input_hash"`
	InputURI          string  `json:"input_uri"`
	InputText         string  `json:"input_text"`
	EscrowDenom       string  `json:"escrow_denom"`
	EscrowAmount      uint64  `json:"escrow_amount"`
	DeadlineHeight    int64   `json:"deadline_height"`
	Status            string  `json:"status"`
	CreatedAtHeight   int64   `json:"created_at_height"`
	FinalizedAtHeight *int64  `json:"finalized_at_height,omitempty"`
	OutputHash        *string `json:"output_hash,omitempty"`
	OutputURI         *string `json:"output_uri,omitempty"`
	OutputText        *string `json:"output_text,omitempty"`
	Provider          *string `json:"provider,omitempty"`
	PaidDenom         *string `json:"paid_denom,omitempty"`
	PaidAmount        *uint64 `json:"paid_amount,omitempty"`
}

func (s *Store) UpsertRequest(r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO requests (id, service_id, requester, input_hash, input_uri, input_text, escrow_denom, escrow_amount, deadline_height, status, created_at_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status=excluded.status,
			updated_at=strftime('%s','now')
	`, r.ID, r.ServiceID, r.Requester, r.InputHash, r.InputURI, r.InputText, r.EscrowDenom, r.EscrowAmount, r.DeadlineHeight, r.Status, r.CreatedAtHeight)
	return err
}

func (s *Store) FinalizeRequest(id uint64, provider, outputHash, outputURI, outputText, paidDenom string, paidAmount uint64, height int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		UPDATE requests SET
			status = 'FINALIZED',
			finalized_at_height = ?,
			output_hash = ?, output_uri = ?, output_text = ?,
			provider = ?, paid_denom = ?, paid_amount = ?,
			updated_at = strftime('%s','now')
		WHERE id = ?
	`, height, outputHash, outputURI, outputText, provider, paidDenom, paidAmount, id)
	return err
}

func (s *Store) RefundRequest(id uint64, height int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE requests SET status='REFUNDED', finalized_at_height=?, updated_at=strftime('%s','now') WHERE id = ?`, height, id)
	return err
}

func (s *Store) GetRequest(id uint64) (Request, error) {
	row := s.db.QueryRow(`SELECT id, service_id, requester, input_hash, input_uri, input_text,
		escrow_denom, escrow_amount, deadline_height, status, created_at_height,
		finalized_at_height, output_hash, output_uri, output_text, provider, paid_denom, paid_amount
		FROM requests WHERE id = ?`, id)
	var r Request
	if err := row.Scan(&r.ID, &r.ServiceID, &r.Requester, &r.InputHash, &r.InputURI, &r.InputText,
		&r.EscrowDenom, &r.EscrowAmount, &r.DeadlineHeight, &r.Status, &r.CreatedAtHeight,
		&r.FinalizedAtHeight, &r.OutputHash, &r.OutputURI, &r.OutputText, &r.Provider, &r.PaidDenom, &r.PaidAmount); err != nil {
		if err == sql.ErrNoRows {
			return Request{}, ErrNotFound
		}
		return Request{}, err
	}
	return r, nil
}

func (s *Store) ListRequests(status string, serviceID uint64) ([]Request, error) {
	q := `SELECT id, service_id, requester, input_hash, input_uri, input_text,
		escrow_denom, escrow_amount, deadline_height, status, created_at_height,
		finalized_at_height, output_hash, output_uri, output_text, provider, paid_denom, paid_amount
		FROM requests WHERE 1=1`
	var args []any
	if status != "" {
		q += " AND status = ?"
		args = append(args, status)
	}
	if serviceID != 0 {
		q += " AND service_id = ?"
		args = append(args, serviceID)
	}
	q += " ORDER BY id DESC LIMIT 200"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.ServiceID, &r.Requester, &r.InputHash, &r.InputURI, &r.InputText,
			&r.EscrowDenom, &r.EscrowAmount, &r.DeadlineHeight, &r.Status, &r.CreatedAtHeight,
			&r.FinalizedAtHeight, &r.OutputHash, &r.OutputURI, &r.OutputText, &r.Provider, &r.PaidDenom, &r.PaidAmount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─── Stats ──────────────────────────────────────────────────────────────────

type Stats struct {
	ServicesTotal     int64 `json:"services_total"`
	RequestsTotal     int64 `json:"requests_total"`
	RequestsFinalized int64 `json:"requests_finalized"`
	RequestsPending   int64 `json:"requests_pending"`
	RequestsRefunded  int64 `json:"requests_refunded"`
}

func (s *Store) StatsSummary() (Stats, error) {
	const q = `SELECT
		(SELECT COUNT(*) FROM services),
		(SELECT COUNT(*) FROM requests),
		(SELECT COUNT(*) FROM requests WHERE status='FINALIZED'),
		(SELECT COUNT(*) FROM requests WHERE status='PENDING'),
		(SELECT COUNT(*) FROM requests WHERE status='REFUNDED')`
	var st Stats
	if err := s.db.QueryRow(q).Scan(&st.ServicesTotal, &st.RequestsTotal, &st.RequestsFinalized, &st.RequestsPending, &st.RequestsRefunded); err != nil {
		return Stats{}, err
	}
	return st, nil
}

// ErrNotFound is returned for missing rows.
var ErrNotFound = errNotFound("not found")

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

// silence unused-imports linter for json (kept for future encode helpers).
var _ = json.Marshal
