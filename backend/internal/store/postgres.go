package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wear"
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
SELECT car_id, vehicle_name, entry_et, exit_et, fuel_added, tyres_changed, driver_swap, stopped, work, fuel_time
FROM stops WHERE session_id = $1 ORDER BY entry_et`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StopRecord
	for rows.Next() {
		s := StopRecord{SessionID: sessionID}
		if err := rows.Scan(&s.CarID, &s.VehicleName, &s.EntryET, &s.ExitET, &s.FuelAdded, &s.TyresChanged, &s.DriverSwap, &s.Stopped, &s.Work, &s.FuelTime); err != nil {
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

func (p *Postgres) GetTrackMap(ctx context.Context, track string) (trackmap.Map, bool, error) {
	var m trackmap.Map
	var points []byte
	err := p.pool.QueryRow(ctx, `SELECT track, length, points FROM track_maps WHERE track = $1`, track).
		Scan(&m.Track, &m.Length, &points)
	if errors.Is(err, pgx.ErrNoRows) {
		return trackmap.Map{}, false, nil
	}
	if err != nil {
		return trackmap.Map{}, false, err
	}
	if err := json.Unmarshal(points, &m.Points); err != nil {
		return trackmap.Map{}, false, err
	}
	return m, true, nil
}

func (p *Postgres) SaveTrackMap(ctx context.Context, m trackmap.Map) error {
	points, err := json.Marshal(m.Points)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO track_maps (track, length, points) VALUES ($1,$2,$3)
ON CONFLICT (track) DO UPDATE SET length = EXCLUDED.length, points = EXCLUDED.points`, m.Track, m.Length, points)
	return err
}

func (p *Postgres) DeleteTrackMap(ctx context.Context, track string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM track_maps WHERE track = $1`, track)
	return err
}

func (p *Postgres) ListTracks(ctx context.Context) ([]TrackInfo, error) {
	rows, err := p.pool.Query(ctx, `SELECT track, length FROM track_maps ORDER BY track`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackInfo{}
	for rows.Next() {
		var t TrackInfo
		if err := rows.Scan(&t.Track, &t.Length); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *Postgres) GetCorners(ctx context.Context, track string) ([]Corner, error) {
	var raw []byte
	err := p.pool.QueryRow(ctx, `SELECT corners FROM track_corners WHERE track = $1`, track).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Corner{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Corner{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Postgres) SaveCorners(ctx context.Context, track string, corners []Corner) error {
	raw, err := json.Marshal(corners)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO track_corners (track, corners) VALUES ($1,$2)
ON CONFLICT (track) DO UPDATE SET corners = EXCLUDED.corners`, track, raw)
	return err
}

func (p *Postgres) SaveLapWear(ctx context.Context, sessionID string, carID int, vehicleName string, w wear.LapWear) error {
	loss, err := json.Marshal(w.Loss)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO wear_laps (session_id, car_id, vehicle_name, lap, coverage, loss) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (session_id, car_id, vehicle_name, lap) DO UPDATE SET coverage = EXCLUDED.coverage, loss = EXCLUDED.loss`,
		sessionID, carID, vehicleName, w.Lap, w.Coverage, loss)
	return err
}

func (p *Postgres) LapWears(ctx context.Context, sessionID string, carID int, vehicleName string) ([]wear.LapWear, error) {
	rows, err := p.pool.Query(ctx, `
SELECT lap, coverage, loss FROM wear_laps WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3 ORDER BY lap`,
		sessionID, carID, vehicleName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wear.LapWear
	for rows.Next() {
		var w wear.LapWear
		var loss []byte
		if err := rows.Scan(&w.Lap, &w.Coverage, &loss); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(loss, &w.Loss); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *Postgres) AddStopped(ctx context.Context, sessionID string, carID int, vehicleName string, st Standing) error {
	if st.To <= st.From {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
UPDATE stops SET stopped = stopped + $5 - $4, work = work + $6, fuel_time = fuel_time + $7
WHERE id = (SELECT id FROM stops
            WHERE session_id = $1 AND car_id = $2 AND vehicle_name = $3 AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, carID, vehicleName, st.From, st.To, st.Work, st.FuelTime)
	return err
}

func (p *Postgres) PitSetups(ctx context.Context, car string) ([]PitSetup, error) {
	rows, err := p.pool.Query(ctx, `SELECT car, name, steps FROM pit_setups WHERE car = $1 ORDER BY seq`, car)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PitSetup{}
	for rows.Next() {
		var s PitSetup
		var steps []byte
		if err := rows.Scan(&s.Car, &s.Name, &steps); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(steps, &s.Steps); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) SavePitSetup(ctx context.Context, s PitSetup) error {
	steps, err := json.Marshal(s.Steps)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO pit_setups (car, name, steps) VALUES ($1,$2,$3)
ON CONFLICT (car, name) DO UPDATE SET steps = EXCLUDED.steps`, s.Car, s.Name, steps)
	return err
}

func (p *Postgres) DeletePitSetup(ctx context.Context, car, name string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM pit_setups WHERE car = $1 AND name = $2`, car, name)
	return err
}

func (p *Postgres) TouchSession(ctx context.Context, s SessionInfo) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO sessions (session_id, track, session_type, car, car_id, started_at, last_seen, last_et)
VALUES ($1,$2,$3,$4,$7,$5,$5,$6)
ON CONFLICT (session_id) DO UPDATE SET
    track = COALESCE(NULLIF(EXCLUDED.track, ''), sessions.track),
    session_type = COALESCE(NULLIF(EXCLUDED.session_type, ''), sessions.session_type),
    car = COALESCE(NULLIF(EXCLUDED.car, ''), sessions.car),
    car_id = CASE WHEN EXCLUDED.car_id >= 0 THEN EXCLUDED.car_id ELSE sessions.car_id END,
    last_seen = EXCLUDED.last_seen,
    last_et = EXCLUDED.last_et`,
		s.SessionID, s.Track, s.SessionType, s.Car, s.LastSeen, s.LastET, s.CarID)
	return err
}

func (p *Postgres) Sessions(ctx context.Context) ([]SessionSummary, error) {
	rows, err := p.pool.Query(ctx, `
SELECT s.session_id, s.track, s.session_type, s.car, s.car_id, s.started_at, s.last_seen, s.last_et,
  (SELECT count(*) FROM laps l WHERE l.session_id = s.session_id AND l.vehicle_name = s.car AND (s.car_id < 0 OR l.car_id = s.car_id)),
  (SELECT count(*) FROM stops t WHERE t.session_id = s.session_id AND t.vehicle_name = s.car AND (s.car_id < 0 OR t.car_id = s.car_id)),
  (SELECT count(*) FROM lap_conditions c WHERE c.session_id = s.session_id),
  (SELECT count(*) FROM lap_conditions c WHERE c.session_id = s.session_id AND c.control = 1),
  (SELECT count(*) FROM lap_conditions c WHERE c.session_id = s.session_id AND c.control = 0)
FROM sessions s ORDER BY s.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionSummary{}
	for rows.Next() {
		var s SessionSummary
		if err := rows.Scan(&s.SessionID, &s.Track, &s.SessionType, &s.Car, &s.CarID, &s.StartedAt, &s.LastSeen, &s.LastET,
			&s.Laps, &s.Stops, &s.Conditions, &s.AILaps, &s.PlayerLaps); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) SaveLapConditions(ctx context.Context, c LapConditions) error {
	arr := func(v []float64) []byte {
		if v == nil {
			return nil
		}
		b, _ := json.Marshal(v)
		return b
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO lap_conditions (session_id, car_id, vehicle_name, lap, et, control, ambient, track, rain, wetness,
  surface, inner_temp, pressure, brake, water, oil, compound_front, compound_rear)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (session_id, car_id, vehicle_name, lap) DO UPDATE SET
  et = EXCLUDED.et, control = EXCLUDED.control, ambient = EXCLUDED.ambient, track = EXCLUDED.track,
  rain = EXCLUDED.rain, wetness = EXCLUDED.wetness, surface = EXCLUDED.surface, inner_temp = EXCLUDED.inner_temp,
  pressure = EXCLUDED.pressure, brake = EXCLUDED.brake, water = EXCLUDED.water, oil = EXCLUDED.oil,
  compound_front = EXCLUDED.compound_front, compound_rear = EXCLUDED.compound_rear`,
		c.SessionID, c.CarID, c.VehicleName, c.Lap, c.ET, c.Control, c.Ambient, c.Track, c.Rain, c.Wetness,
		arr(c.Surface), arr(c.Inner), arr(c.Pressure), arr(c.Brake), c.Water, c.Oil, c.CompoundFront, c.CompoundRear)
	return err
}

func (p *Postgres) LapConditions(ctx context.Context, sessionID string) ([]LapConditions, error) {
	rows, err := p.pool.Query(ctx, `
SELECT car_id, vehicle_name, lap, et, control, ambient, track, rain, wetness, surface, inner_temp, pressure, brake,
  water, oil, compound_front, compound_rear
FROM lap_conditions WHERE session_id = $1 ORDER BY car_id, lap`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LapConditions
	for rows.Next() {
		c := LapConditions{SessionID: sessionID}
		var ctrl *int16
		var surface, inner, pressure, brake []byte
		if err := rows.Scan(&c.CarID, &c.VehicleName, &c.Lap, &c.ET, &ctrl, &c.Ambient, &c.Track, &c.Rain, &c.Wetness,
			&surface, &inner, &pressure, &brake, &c.Water, &c.Oil, &c.CompoundFront, &c.CompoundRear); err != nil {
			return nil, err
		}
		if ctrl != nil {
			v := int(*ctrl)
			c.Control = &v
		}
		for dst, raw := range map[*[]float64][]byte{&c.Surface: surface, &c.Inner: inner, &c.Pressure: pressure, &c.Brake: brake} {
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, dst); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) SaveWeatherPoint(ctx context.Context, sessionID string, w WeatherPoint) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO weather_points (session_id, et, ambient, track, rain, wetness) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (session_id, et) DO UPDATE SET ambient = EXCLUDED.ambient, track = EXCLUDED.track,
  rain = EXCLUDED.rain, wetness = EXCLUDED.wetness`, sessionID, w.ET, w.Ambient, w.Track, w.Rain, w.Wetness)
	return err
}

func (p *Postgres) WeatherPoints(ctx context.Context, sessionID string, limit int) ([]WeatherPoint, error) {
	rows, err := p.pool.Query(ctx, `
SELECT et, ambient, track, rain, wetness FROM (
  SELECT et, ambient, track, rain, wetness FROM weather_points WHERE session_id = $1 ORDER BY et DESC LIMIT $2
) newest ORDER BY et`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeatherPoint
	for rows.Next() {
		var w WeatherPoint
		if err := rows.Scan(&w.ET, &w.Ambient, &w.Track, &w.Rain, &w.Wetness); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *Postgres) GetPitLane(ctx context.Context, track string) (pitlane.Lane, bool, error) {
	var data []byte
	err := p.pool.QueryRow(ctx, `SELECT data FROM track_pits WHERE track = $1`, track).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return pitlane.Lane{}, false, nil
	}
	if err != nil {
		return pitlane.Lane{}, false, err
	}
	var l pitlane.Lane
	if err := json.Unmarshal(data, &l); err != nil {
		return pitlane.Lane{}, false, err
	}
	l.Track = track
	return l, true, nil
}

func (p *Postgres) SavePitLane(ctx context.Context, l pitlane.Lane) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO track_pits (track, data) VALUES ($1,$2)
ON CONFLICT (track) DO UPDATE SET data = EXCLUDED.data`, l.Track, data)
	return err
}
