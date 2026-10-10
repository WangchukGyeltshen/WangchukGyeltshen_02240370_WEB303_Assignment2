// repository.go
// Database access for the Permit Service. This code touches ONLY
// permits_db. Route data is never read from the database here; it is
// fetched from Route Service over gRPC (see routeclient.go).
package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	pb "trekking/proto/permit"
)

var ErrNotFound = errors.New("permit not found")

const dateLayout = "2006-01-02"

const permitColumns = "id, route_id, trekker_name, email, group_size, start_date, status, created_at"

type PermitRepository struct {
	db *sql.DB
}

func NewPermitRepository(db *sql.DB) *PermitRepository {
	return &PermitRepository{db: db}
}

// InitSchema creates the permits table. route_id has no foreign key
// because routes live in a different database owned by another service.
func (r *PermitRepository) InitSchema(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS permits (
			id           SERIAL PRIMARY KEY,
			route_id     BIGINT NOT NULL,
			trekker_name TEXT   NOT NULL,
			email        TEXT   NOT NULL,
			group_size   INT    NOT NULL,
			start_date   DATE   NOT NULL,
			status       TEXT   NOT NULL DEFAULT 'PENDING',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPermit(s scanner) (*pb.Permit, error) {
	var p pb.Permit
	var start, created time.Time
	err := s.Scan(&p.Id, &p.RouteId, &p.TrekkerName, &p.Email,
		&p.GroupSize, &start, &p.Status, &created)
	if err != nil {
		return nil, err
	}
	p.StartDate = start.Format(dateLayout)
	p.CreatedAt = created.Format(time.RFC3339)
	return &p, nil
}

func (r *PermitRepository) Create(ctx context.Context, in *pb.CreatePermitRequest) (*pb.Permit, error) {
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO permits (route_id, trekker_name, email, group_size, start_date, status)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
		RETURNING `+permitColumns,
		in.RouteId, in.TrekkerName, in.Email, in.GroupSize, start)
	return scanPermit(row)
}

func (r *PermitRepository) Get(ctx context.Context, id int64) (*pb.Permit, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+permitColumns+` FROM permits WHERE id = $1`, id)
	p, err := scanPermit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (r *PermitRepository) List(ctx context.Context) ([]*pb.Permit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+permitColumns+` FROM permits ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permits := []*pb.Permit{}
	for rows.Next() {
		p, err := scanPermit(rows)
		if err != nil {
			return nil, err
		}
		permits = append(permits, p)
	}
	return permits, rows.Err()
}

func (r *PermitRepository) Update(ctx context.Context, in *pb.UpdatePermitRequest) (*pb.Permit, error) {
	start, err := time.Parse(dateLayout, in.StartDate)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE permits
		SET route_id = $1, trekker_name = $2, email = $3,
		    group_size = $4, start_date = $5, status = $6
		WHERE id = $7
		RETURNING `+permitColumns,
		in.RouteId, in.TrekkerName, in.Email, in.GroupSize, start, in.Status, in.Id)
	p, err := scanPermit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (r *PermitRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM permits WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}