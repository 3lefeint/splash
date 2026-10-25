package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/3lefeint/splash/backend/internal/wire"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Postgres is the Store backed by PostgreSQL.
type Postgres struct{ pool *pgxpool.Pool }

// NewPostgres wraps a connection pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool} }

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

func (p *Postgres) SaveLap(ctx context.Context, sessionID string, l wire.Lap) error {
	var w [4]*float64
	if len(l.Wear) == 4 {
		for i := range w {
			w[i] = &l.Wear[i]
		}
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO laps (session_id, car_id, vehicle_name, lap, driver, lap_time, s1, s2, s3,
                  fuel_start, fuel_end, tyre_age, wear_fl, wear_fr, wear_rl, wear_rr, fcy, in_lap, out_lap)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (session_id, car_id, vehicle_name, lap) DO UPDATE SET
    driver     = COALESCE(NULLIF(EXCLUDED.driver, ''), laps.driver),
    lap_time   = COALESCE(EXCLUDED.lap_time, laps.lap_time),
    s1         = COALESCE(EXCLUDED.s1, laps.s1),
    s2         = COALESCE(EXCLUDED.s2, laps.s2),
    s3         = COALESCE(EXCLUDED.s3, laps.s3),
    fuel_start = COALESCE(EXCLUDED.fuel_start, laps.fuel_start),
    fuel_end   = COALESCE(EXCLUDED.fuel_end, laps.fuel_end),
    tyre_age   = COALESCE(EXCLUDED.tyre_age, laps.tyre_age),
    wear_fl    = COALESCE(EXCLUDED.wear_fl, laps.wear_fl),
    wear_fr    = COALESCE(EXCLUDED.wear_fr, laps.wear_fr),
    wear_rl    = COALESCE(EXCLUDED.wear_rl, laps.wear_rl),
    wear_rr    = COALESCE(EXCLUDED.wear_rr, laps.wear_rr),
    fcy        = laps.fcy OR EXCLUDED.fcy,
    in_lap     = laps.in_lap OR EXCLUDED.in_lap,
    out_lap    = laps.out_lap OR EXCLUDED.out_lap`,
		sessionID, l.CarID, l.VehicleName, l.Lap, l.Driver, l.LapTime, l.S1, l.S2, l.S3,
		l.FuelStart, l.FuelEnd, l.TyreAge, w[0], w[1], w[2], w[3], l.FCY, l.InLap, l.OutLap)
	return err
}

func (p *Postgres) SaveEvent(ctx context.Context, sessionID string, et float64, e wire.Event) error {
	var err error
	switch e.Kind {
	case wire.EventPitEntry:
		_, err = p.pool.Exec(ctx, `
INSERT INTO stops (session_id, car_id, vehicle_name, entry_et)
SELECT $1, $2, $3, $4::double precision
WHERE NOT EXISTS (SELECT 1 FROM stops
                  WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3
                    AND abs(entry_et - $4::double precision) <= $5)
ON CONFLICT DO NOTHING`, sessionID, e.CarID, e.VehicleName, et, PitEntryWindow)
	case wire.EventPitExit:
		changed := e.TyresChanged != nil && *e.TyresChanged
		_, err = p.pool.Exec(ctx, `
UPDATE stops SET exit_et = $4,
                 fuel_added = COALESCE($5, fuel_added),
                 tyres_changed = tyres_changed OR $6
WHERE id = (SELECT id FROM stops
            WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3
              AND exit_et IS NULL AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, e.CarID, e.VehicleName, et, e.FuelAdded, changed)
	case wire.EventDriverSwap:
		_, err = p.pool.Exec(ctx, `
UPDATE stops SET driver_swap = true
WHERE id = (SELECT id FROM stops
            WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3 AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, e.CarID, e.VehicleName, et)
	}
	return err
}

func (p *Postgres) Progress(ctx context.Context, agentID, sessionID string) (int64, bool, error) {
	var seq int64
	err := p.pool.QueryRow(ctx, `SELECT last_seq FROM agent_progress WHERE agent_id = $1 AND session_id = $2`,
		agentID, sessionID).Scan(&seq)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	return seq, err == nil, err
}

func (p *Postgres) SetProgress(ctx context.Context, agentID, sessionID string, seq int64) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO agent_progress (agent_id, session_id, last_seq) VALUES ($1,$2,$3)
ON CONFLICT (agent_id, session_id) DO UPDATE SET last_seq = GREATEST(agent_progress.last_seq, EXCLUDED.last_seq)`,
		agentID, sessionID, seq)
	return err
}

func (p *Postgres) Laps(ctx context.Context, sessionID string) ([]LapRecord, error) {
	rows, err := p.pool.Query(ctx, lapSelect+` WHERE session_id = $1 ORDER BY car_id, lap`, sessionID)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows, sessionID)
}

func (p *Postgres) Stops(ctx context.Context, sessionID string) ([]StopRecord, error) {
	rows, err := p.pool.Query(ctx, `
SELECT car_id, vehicle_name, entry_et, exit_et, fuel_added, tyres_changed, driver_swap
FROM stops WHERE session_id = $1 ORDER BY entry_et`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StopRecord
	for rows.Next() {
		s := StopRecord{SessionID: sessionID}
		if err := rows.Scan(&s.CarID, &s.VehicleName, &s.EntryET, &s.ExitET, &s.FuelAdded, &s.TyresChanged, &s.DriverSwap); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Connect opens a pool for the given database URL.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func (p *Postgres) LapsOfCar(ctx context.Context, sessionID string, carID int, vehicleName string) ([]LapRecord, error) {
	rows, err := p.pool.Query(ctx, lapSelect+` WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3 ORDER BY lap`,
		sessionID, carID, vehicleName)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows, sessionID)
}

func (p *Postgres) GetParams(ctx context.Context, track, car string) (Params, bool, error) {
	rows, err := p.pool.Query(ctx, paramsSelect+` WHERE track = $1 AND car = $2`, track, car)
	if err != nil {
		return Params{}, false, err
	}
	list, err := scanParams(rows)
	if err != nil || len(list) == 0 {
		return Params{}, false, err
	}
	return list[0], true, nil
}

func (p *Postgres) ListParams(ctx context.Context) ([]Params, error) {
	rows, err := p.pool.Query(ctx, paramsSelect+` ORDER BY track, car`)
	if err != nil {
		return nil, err
	}
	return scanParams(rows)
}

func (p *Postgres) SetParams(ctx context.Context, v Params) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO params (track, car, pit_through_green, pit_through_fcy, refuel_rate, tyre_change, driver_swap,
                    parallel_service, fuel_effect, deg_per_lap, save_cost, reserve)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (track, car) DO UPDATE SET
    pit_through_green = EXCLUDED.pit_through_green, pit_through_fcy = EXCLUDED.pit_through_fcy,
    refuel_rate = EXCLUDED.refuel_rate, tyre_change = EXCLUDED.tyre_change, driver_swap = EXCLUDED.driver_swap,
    parallel_service = EXCLUDED.parallel_service, fuel_effect = EXCLUDED.fuel_effect,
    deg_per_lap = EXCLUDED.deg_per_lap, save_cost = EXCLUDED.save_cost, reserve = EXCLUDED.reserve`,
		v.Track, v.Car, v.PitThroughGreen, v.PitThroughFCY, v.RefuelRate, v.TyreChange, v.DriverSwap,
		v.ParallelService, v.FuelEffect, v.DegPerLap, v.SaveCost, v.Reserve)
	return err
}

func (p *Postgres) GetStintPlan(ctx context.Context) ([]PlanRow, error) {
	rows, err := p.pool.Query(ctx, `SELECT driver, duration FROM stint_plan ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanRow
	for rows.Next() {
		var r PlanRow
		if err := rows.Scan(&r.Driver, &r.Duration); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) SetStintPlan(ctx context.Context, plan []PlanRow) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM stint_plan`); err != nil {
		return err
	}
	for i, r := range plan {
		if _, err := tx.Exec(ctx, `INSERT INTO stint_plan (position, driver, duration) VALUES ($1,$2,$3)`,
			i+1, r.Driver, r.Duration); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetOverrides(ctx context.Context) (map[string]Override, error) {
	rows, err := p.pool.Query(ctx, `SELECT key, value, set_at FROM overrides`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Override{}
	for rows.Next() {
		var o Override
		if err := rows.Scan(&o.Key, &o.Value, &o.SetAt); err != nil {
			return nil, err
		}
		out[o.Key] = o
	}
	return out, rows.Err()
}

func (p *Postgres) SetOverride(ctx context.Context, key string, value float64) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO overrides (key, value) VALUES ($1,$2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = now()`, key, value)
	return err
}

func (p *Postgres) DeleteOverride(ctx context.Context, key string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM overrides WHERE key = $1`, key)
	return err
}

const paramsSelect = `SELECT track, car, pit_through_green, pit_through_fcy, refuel_rate, tyre_change, driver_swap,
       parallel_service, fuel_effect, deg_per_lap, save_cost, reserve FROM params`

func scanParams(rows pgx.Rows) ([]Params, error) {
	defer rows.Close()
	var out []Params
	for rows.Next() {
		var v Params
		if err := rows.Scan(&v.Track, &v.Car, &v.PitThroughGreen, &v.PitThroughFCY, &v.RefuelRate, &v.TyreChange,
			&v.DriverSwap, &v.ParallelService, &v.FuelEffect, &v.DegPerLap, &v.SaveCost, &v.Reserve); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const lapSelect = `SELECT car_id, vehicle_name, lap, driver, lap_time, s1, s2, s3, fuel_start, fuel_end, tyre_age,
       wear_fl, wear_fr, wear_rl, wear_rr, fcy, in_lap, out_lap FROM laps`

func scanLaps(rows pgx.Rows, sessionID string) ([]LapRecord, error) {
	defer rows.Close()
	var out []LapRecord
	for rows.Next() {
		var l wire.Lap
		var w [4]*float64
		if err := rows.Scan(&l.CarID, &l.VehicleName, &l.Lap, &l.Driver, &l.LapTime, &l.S1, &l.S2, &l.S3,
			&l.FuelStart, &l.FuelEnd, &l.TyreAge, &w[0], &w[1], &w[2], &w[3], &l.FCY, &l.InLap, &l.OutLap); err != nil {
			return nil, err
		}
		if w[0] != nil && w[1] != nil && w[2] != nil && w[3] != nil {
			l.Wear = []float64{*w[0], *w[1], *w[2], *w[3]}
		}
		out = append(out, LapRecord{sessionID, l})
	}
	return out, rows.Err()
}
