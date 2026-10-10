// repository.go
// Database access for the Route Service. This is the ONLY code that
// touches routes_db (Database per Service pattern).
package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	pb "trekking/proto/route"
)

// Errors returned by the repository, mapped to gRPC codes in server.go.
var (
	ErrNotFound  = errors.New("route not found")
	ErrDuplicate = errors.New("a route with this name already exists")
)

// routeColumns is the column list used in every SELECT/RETURNING.
const routeColumns = "id, name, dzongkhag, difficulty, duration_days, max_altitude_m, max_group_size, created_at"

type RouteRepository struct {
	db *sql.DB
}

func NewRouteRepository(db *sql.DB) *RouteRepository {
	return &RouteRepository{db: db}
}

// InitSchema creates the routes table if it does not exist yet.
func (r *RouteRepository) InitSchema(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS routes (
			id             SERIAL PRIMARY KEY,
			name           TEXT NOT NULL UNIQUE,
			dzongkhag      TEXT NOT NULL,
			difficulty     TEXT NOT NULL,
			duration_days  INT  NOT NULL,
			max_altitude_m INT  NOT NULL,
			max_group_size INT  NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	return err
}

// scanner lets scanRoute work with both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRoute(s scanner) (*pb.Route, error) {
	var rt pb.Route
	var created time.Time
	err := s.Scan(&rt.Id, &rt.Name, &rt.Dzongkhag, &rt.Difficulty,
		&rt.DurationDays, &rt.MaxAltitudeM, &rt.MaxGroupSize, &created)
	if err != nil {
		return nil, err
	}
	rt.CreatedAt = created.Format(time.RFC3339)
	return &rt, nil
}

// isUniqueViolation checks for Postgres error 23505 (unique_violation).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *RouteRepository) Create(ctx context.Context, in *pb.CreateRouteRequest) (*pb.Route, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO routes (name, dzongkhag, difficulty, duration_days, max_altitude_m, max_group_size)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+routeColumns,
		in.Name, in.Dzongkhag, in.Difficulty, in.DurationDays, in.MaxAltitudeM, in.MaxGroupSize)
	rt, err := scanRoute(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicate
	}
	return rt, err
}

func (r *RouteRepository) Get(ctx context.Context, id int64) (*pb.Route, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+routeColumns+` FROM routes WHERE id = $1`, id)
	rt, err := scanRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rt, err
}

func (r *RouteRepository) List(ctx context.Context) ([]*pb.Route, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+routeColumns+` FROM routes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []*pb.Route{}
	for rows.Next() {
		rt, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, rt)
	}
	return routes, rows.Err()
}

func (r *RouteRepository) Update(ctx context.Context, in *pb.UpdateRouteRequest) (*pb.Route, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE routes
		SET name = $1, dzongkhag = $2, difficulty = $3,
		    duration_days = $4, max_altitude_m = $5, max_group_size = $6
		WHERE id = $7
		RETURNING `+routeColumns,
		in.Name, in.Dzongkhag, in.Difficulty, in.DurationDays, in.MaxAltitudeM, in.MaxGroupSize, in.Id)
	rt, err := scanRoute(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case isUniqueViolation(err):
		return nil, ErrDuplicate
	}
	return rt, err
}

func (r *RouteRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM routes WHERE id = $1`, id)
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