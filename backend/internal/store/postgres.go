package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// Postgres is the Store backed by PostgreSQL. NewPostgres returns the root, which serves the shared track data and
// the Directory; ForTeam returns the Store of one team. The root has team 0, which no row belongs to.
type Postgres struct {
	pool *pgxpool.Pool
	team int64
}

// NewPostgres wraps a connection pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool, 0} }

// ForTeam returns the Store of one team.
func (p *Postgres) ForTeam(id int64) Store { return &Postgres{p.pool, id} }

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

func (p *Postgres) SaveLap(ctx context.Context, sessionID string, l wire.Lap, agentID int64) error {
	var w [4]*float64
	if len(l.Wear) == 4 {
		for i := range w {
			w[i] = &l.Wear[i]
		}
	}
	hasFuel := l.FuelStart != nil || l.FuelEnd != nil
	_, err := p.pool.Exec(ctx, `
INSERT INTO laps (session_id, car_id, vehicle_name, lap, driver, lap_time, s1, s2, s3,
                  fuel_start, fuel_end, tyre_age, wear_fl, wear_fr, wear_rl, wear_rr, fcy, in_lap, out_lap, team_id, agent_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,NULLIF($21::bigint, 0))
ON CONFLICT (team_id, session_id, car_id, vehicle_name, lap) DO UPDATE SET
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
    out_lap    = laps.out_lap OR EXCLUDED.out_lap,
    agent_id   = CASE WHEN $22::boolean THEN COALESCE(EXCLUDED.agent_id, laps.agent_id) ELSE COALESCE(laps.agent_id, EXCLUDED.agent_id) END`,
		sessionID, l.CarID, l.VehicleName, l.Lap, l.Driver, l.LapTime, l.S1, l.S2, l.S3,
		l.FuelStart, l.FuelEnd, l.TyreAge, w[0], w[1], w[2], w[3], l.FCY, l.InLap, l.OutLap, p.team, agentID, hasFuel)
	return err
}

func (p *Postgres) SaveEvent(ctx context.Context, sessionID string, et float64, e wire.Event) error {
	var err error
	switch e.Kind {
	case wire.EventPitEntry:
		_, err = p.pool.Exec(ctx, `
INSERT INTO stops (session_id, car_id, vehicle_name, entry_et, team_id)
SELECT $1, $2, $3, $4::double precision, $6::bigint
WHERE NOT EXISTS (SELECT 1 FROM stops
                  WHERE team_id = $6 AND session_id = $1 AND car_id = $2 AND vehicle_name = $3
                    AND abs(entry_et - $4::double precision) <= $5)
ON CONFLICT DO NOTHING`, sessionID, e.CarID, e.VehicleName, et, PitEntryWindow, p.team)
	case wire.EventPitExit:
		changed := e.TyresChanged != nil && *e.TyresChanged
		_, err = p.pool.Exec(ctx, `
UPDATE stops SET exit_et = $4,
                 fuel_added = COALESCE($5, fuel_added),
                 tyres_changed = tyres_changed OR $6
WHERE id = (SELECT id FROM stops
            WHERE team_id = $7 AND session_id = $1 AND car_id = $2 AND vehicle_name = $3
              AND exit_et IS NULL AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, e.CarID, e.VehicleName, et, e.FuelAdded, changed, p.team)
	case wire.EventDriverSwap:
		_, err = p.pool.Exec(ctx, `
UPDATE stops SET driver_swap = true
WHERE id = (SELECT id FROM stops
            WHERE team_id = $5 AND session_id = $1 AND car_id = $2 AND vehicle_name = $3 AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, e.CarID, e.VehicleName, et, p.team)
	}
	return err
}

func (p *Postgres) Progress(ctx context.Context, agentID, sessionID string) (int64, bool, error) {
	var seq int64
	err := p.pool.QueryRow(ctx, `SELECT last_seq FROM agent_progress WHERE agent_id = $1 AND session_id = $2 AND team_id = $3`,
		agentID, sessionID, p.team).Scan(&seq)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	return seq, err == nil, err
}

func (p *Postgres) SetProgress(ctx context.Context, agentID, sessionID string, seq int64) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO agent_progress (agent_id, session_id, last_seq, team_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (team_id, agent_id, session_id) DO UPDATE SET last_seq = GREATEST(agent_progress.last_seq, EXCLUDED.last_seq)`,
		agentID, sessionID, seq, p.team)
	return err
}

func (p *Postgres) Laps(ctx context.Context, sessionID string) ([]LapRecord, error) {
	rows, err := p.pool.Query(ctx, lapSelect+` WHERE l.team_id = $2 AND l.session_id = $1 ORDER BY l.car_id, l.lap`, sessionID, p.team)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows, sessionID)
}

func (p *Postgres) Stops(ctx context.Context, sessionID string) ([]StopRecord, error) {
	rows, err := p.pool.Query(ctx, `
SELECT car_id, vehicle_name, entry_et, exit_et, fuel_added, tyres_changed, driver_swap, stopped, work, fuel_time
FROM stops WHERE team_id = $2 AND session_id = $1 ORDER BY entry_et`, sessionID, p.team)
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

// Connect opens a pool for the given database URL. A maxConns above zero overrides the pool size from the URL; a
// non-nil tracer sees every call (see QueryLogger).
func Connect(ctx context.Context, url string, maxConns int32, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if tracer != nil {
		cfg.ConnConfig.Tracer = tracer
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
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
	rows, err := p.pool.Query(ctx, lapSelect+` WHERE l.team_id = $4 AND l.session_id = $1 AND l.car_id = $2 AND l.vehicle_name = $3 ORDER BY l.lap`,
		sessionID, carID, vehicleName, p.team)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows, sessionID)
}

func (p *Postgres) GetParams(ctx context.Context, track, car string) (Params, bool, error) {
	canon, err := p.CanonicalCar(ctx, car)
	if err != nil {
		return Params{}, false, err
	}
	// A set stored before the car had a team car is still under the vehicle name.
	for _, name := range []string{canon, car} {
		rows, err := p.pool.Query(ctx, paramsSelect+` WHERE team_id = $3 AND track = $1 AND car = $2`, track, name, p.team)
		if err != nil {
			return Params{}, false, err
		}
		list, err := scanParams(rows)
		if err != nil {
			return Params{}, false, err
		}
		if len(list) > 0 {
			return list[0], true, nil
		}
		if canon == car {
			break
		}
	}
	return Params{}, false, nil
}

func (p *Postgres) ListParams(ctx context.Context) ([]Params, error) {
	rows, err := p.pool.Query(ctx, paramsSelect+` WHERE team_id = $1 ORDER BY track, car`, p.team)
	if err != nil {
		return nil, err
	}
	return scanParams(rows)
}

func (p *Postgres) SetParams(ctx context.Context, v Params) error {
	v = v.WithDefaults()
	canon, err := p.CanonicalCar(ctx, v.Car)
	if err != nil {
		return err
	}
	v.Car = canon
	_, err = p.pool.Exec(ctx, `
INSERT INTO params (track, car, pit_through_green, pit_through_fcy, refuel_rate, tyre_change, driver_swap,
                    fuel_effect, deg_per_lap, save_cost, reserve,
                    fit_min_laps, fit_slow_percent, fit_skip_fcy, fit_skip_wet, team_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (team_id, track, car) DO UPDATE SET
    pit_through_green = EXCLUDED.pit_through_green, pit_through_fcy = EXCLUDED.pit_through_fcy,
    refuel_rate = EXCLUDED.refuel_rate, tyre_change = EXCLUDED.tyre_change, driver_swap = EXCLUDED.driver_swap,
    fuel_effect = EXCLUDED.fuel_effect,
    deg_per_lap = EXCLUDED.deg_per_lap, save_cost = EXCLUDED.save_cost, reserve = EXCLUDED.reserve,
    fit_min_laps = EXCLUDED.fit_min_laps, fit_slow_percent = EXCLUDED.fit_slow_percent,
    fit_skip_fcy = EXCLUDED.fit_skip_fcy, fit_skip_wet = EXCLUDED.fit_skip_wet`,
		v.Track, v.Car, v.PitThroughGreen, v.PitThroughFCY, v.RefuelRate, v.TyreChange, v.DriverSwap,
		v.FuelEffect, v.DegPerLap, v.SaveCost, v.Reserve,
		v.FitMinLaps, v.FitSlowPercent, *v.FitSkipFCY, *v.FitSkipWet, p.team)
	return err
}

func (p *Postgres) GetStintPlan(ctx context.Context) ([]PlanRow, error) {
	rows, err := p.pool.Query(ctx, `SELECT driver, duration FROM stint_plan WHERE team_id = $1 ORDER BY position`, p.team)
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
	if _, err := tx.Exec(ctx, `DELETE FROM stint_plan WHERE team_id = $1`, p.team); err != nil {
		return err
	}
	for i, r := range plan {
		if _, err := tx.Exec(ctx, `INSERT INTO stint_plan (position, driver, duration, team_id) VALUES ($1,$2,$3,$4)`,
			i+1, r.Driver, r.Duration, p.team); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetOverrides(ctx context.Context, agentID int64) (map[string]Override, error) {
	rows, err := p.pool.Query(ctx, `SELECT key, value, set_at FROM overrides WHERE team_id = $1 AND agent_id = $2`, p.team, agentID)
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

func (p *Postgres) SetOverride(ctx context.Context, agentID int64, key string, value float64) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO overrides (key, value, team_id, agent_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (team_id, agent_id, key) DO UPDATE SET value = EXCLUDED.value, set_at = now()`, key, value, p.team, agentID)
	return err
}

func (p *Postgres) DeleteOverride(ctx context.Context, agentID int64, key string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM overrides WHERE team_id = $2 AND agent_id = $3 AND key = $1`, key, p.team, agentID)
	return err
}

const paramsSelect = `SELECT track, car, pit_through_green, pit_through_fcy, refuel_rate, tyre_change, driver_swap,
       fuel_effect, deg_per_lap, save_cost, reserve,
       fit_min_laps, fit_slow_percent, fit_skip_fcy, fit_skip_wet FROM params`

func scanParams(rows pgx.Rows) ([]Params, error) {
	defer rows.Close()
	var out []Params
	for rows.Next() {
		var v Params
		var skipFCY, skipWet bool
		if err := rows.Scan(&v.Track, &v.Car, &v.PitThroughGreen, &v.PitThroughFCY, &v.RefuelRate, &v.TyreChange,
			&v.DriverSwap, &v.FuelEffect, &v.DegPerLap, &v.SaveCost, &v.Reserve,
			&v.FitMinLaps, &v.FitSlowPercent, &skipFCY, &skipWet); err != nil {
			return nil, err
		}
		v.FitSkipFCY, v.FitSkipWet = &skipFCY, &skipWet
		out = append(out, v.WithDefaults())
	}
	return out, rows.Err()
}

const lapSelect = `SELECT l.car_id, l.vehicle_name, l.lap, l.driver, l.lap_time, l.s1, l.s2, l.s3, l.fuel_start, l.fuel_end, l.tyre_age,
       l.wear_fl, l.wear_fr, l.wear_rl, l.wear_rr, l.fcy, l.in_lap, l.out_lap, l.agent_id, l.excluded,
       COALESCE(l.excluded, s.excluded, false)
FROM laps l LEFT JOIN sessions s ON s.team_id = l.team_id AND s.session_id = l.session_id`

// lapScan holds the columns of lapSelect while a row is read.
type lapScan struct {
	l        wire.Lap
	w        [4]*float64
	agent    *int64
	flag     *bool
	excluded bool
}

func (c *lapScan) targets() []any {
	l := &c.l
	return []any{&l.CarID, &l.VehicleName, &l.Lap, &l.Driver, &l.LapTime, &l.S1, &l.S2, &l.S3, &l.FuelStart, &l.FuelEnd,
		&l.TyreAge, &c.w[0], &c.w[1], &c.w[2], &c.w[3], &l.FCY, &l.InLap, &l.OutLap, &c.agent, &c.flag, &c.excluded}
}

func (c *lapScan) record(sessionID string) LapRecord {
	if c.w[0] != nil && c.w[1] != nil && c.w[2] != nil && c.w[3] != nil {
		c.l.Wear = []float64{*c.w[0], *c.w[1], *c.w[2], *c.w[3]}
	}
	rec := LapRecord{SessionID: sessionID, Lap: c.l, ExcludedFlag: c.flag, Excluded: c.excluded}
	if c.agent != nil {
		rec.AgentID = *c.agent
	}
	return rec
}

func scanLaps(rows pgx.Rows, sessionID string) ([]LapRecord, error) {
	defer rows.Close()
	var out []LapRecord
	for rows.Next() {
		var c lapScan
		if err := rows.Scan(c.targets()...); err != nil {
			return nil, err
		}
		out = append(out, c.record(sessionID))
	}
	return out, rows.Err()
}

func (p *Postgres) HistoryLaps(ctx context.Context, track, vehicleName, exceptSession string) ([]HistoryLap, error) {
	rows, err := p.pool.Query(ctx, `
SELECT l.session_id, s.started_at, c.rain, c.wetness,
       l.car_id, l.vehicle_name, l.lap, l.driver, l.lap_time, l.s1, l.s2, l.s3, l.fuel_start, l.fuel_end, l.tyre_age,
       l.wear_fl, l.wear_fr, l.wear_rl, l.wear_rr, l.fcy, l.in_lap, l.out_lap, l.agent_id, l.excluded, false
FROM laps l
JOIN sessions s ON s.team_id = l.team_id AND s.session_id = l.session_id
LEFT JOIN lap_conditions c ON c.team_id = l.team_id AND c.session_id = l.session_id AND c.car_id = l.car_id
                          AND c.vehicle_name = l.vehicle_name AND c.lap = l.lap
WHERE l.team_id = $1 AND s.track = $2 AND l.session_id <> $4
  AND l.vehicle_name IN (
        SELECT n2.vehicle_name FROM team_car_names n1 JOIN team_car_names n2 ON n2.car_id = n1.car_id
        WHERE n1.team_id = $1 AND n1.vehicle_name = $3
        UNION SELECT $3)
  AND l.fuel_start IS NOT NULL AND l.fuel_end IS NOT NULL
  AND NOT COALESCE(l.excluded, s.excluded, false)
ORDER BY s.started_at, l.session_id, l.car_id, l.lap`, p.team, track, vehicleName, exceptSession)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryLap
	for rows.Next() {
		var h HistoryLap
		var session string
		var c lapScan
		if err := rows.Scan(append([]any{&session, &h.StartedAt, &h.Rain, &h.Wetness}, c.targets()...)...); err != nil {
			return nil, err
		}
		h.LapRecord = c.record(session)
		out = append(out, h)
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
INSERT INTO wear_laps (session_id, car_id, vehicle_name, lap, coverage, loss, team_id) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (team_id, session_id, car_id, vehicle_name, lap) DO UPDATE SET coverage = EXCLUDED.coverage, loss = EXCLUDED.loss`,
		sessionID, carID, vehicleName, w.Lap, w.Coverage, loss, p.team)
	return err
}

func (p *Postgres) LapWears(ctx context.Context, sessionID string, carID int, vehicleName string) ([]wear.LapWear, error) {
	rows, err := p.pool.Query(ctx, `
SELECT lap, coverage, loss FROM wear_laps WHERE team_id = $4 AND session_id = $1 AND car_id = $2 AND vehicle_name = $3 ORDER BY lap`,
		sessionID, carID, vehicleName, p.team)
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
            WHERE team_id = $8 AND session_id = $1 AND car_id = $2 AND vehicle_name = $3 AND entry_et <= $4
            ORDER BY entry_et DESC LIMIT 1)`, sessionID, carID, vehicleName, st.From, st.To, st.Work, st.FuelTime, p.team)
	return err
}

func (p *Postgres) PitSetups(ctx context.Context, car string) ([]PitSetup, error) {
	canon, err := p.CanonicalCar(ctx, car)
	if err != nil {
		return nil, err
	}
	// Set-ups stored before the car had a team car are still under the vehicle name; a set-up of the team car wins.
	rows, err := p.pool.Query(ctx, `SELECT car, name, steps FROM pit_setups WHERE team_id = $3 AND car IN ($1, $2)
ORDER BY (car = $1) DESC, seq`, canon, car, p.team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PitSetup{}
	seen := map[string]bool{}
	for rows.Next() {
		var s PitSetup
		var steps []byte
		if err := rows.Scan(&s.Car, &s.Name, &steps); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
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
	if s.Car, err = p.CanonicalCar(ctx, s.Car); err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO pit_setups (car, name, steps, team_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (team_id, car, name) DO UPDATE SET steps = EXCLUDED.steps`, s.Car, s.Name, steps, p.team)
	return err
}

func (p *Postgres) DeletePitSetup(ctx context.Context, car, name string) error {
	canon, err := p.CanonicalCar(ctx, car)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `DELETE FROM pit_setups WHERE team_id = $3 AND car IN ($1, $4) AND name = $2`, canon, name, p.team, car)
	return err
}

func (p *Postgres) CanonicalCar(ctx context.Context, vehicleName string) (string, error) {
	var name string
	err := p.pool.QueryRow(ctx, `
SELECT c.name FROM team_car_names n JOIN team_cars c ON c.id = n.car_id
WHERE n.team_id = $1 AND n.vehicle_name = $2`, p.team, vehicleName).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicleName, nil
	}
	return name, err
}

func (p *Postgres) Cars(ctx context.Context) ([]TeamCar, error) {
	rows, err := p.pool.Query(ctx, `
SELECT c.id, c.name, COALESCE(array_agg(n.vehicle_name ORDER BY n.vehicle_name) FILTER (WHERE n.vehicle_name IS NOT NULL), '{}')
FROM team_cars c LEFT JOIN team_car_names n ON n.car_id = c.id
WHERE c.team_id = $1 GROUP BY c.id ORDER BY c.name`, p.team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamCar{}
	for rows.Next() {
		var c TeamCar
		if err := rows.Scan(&c.ID, &c.Name, &c.Names); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateCar(ctx context.Context, name string) (TeamCar, error) {
	c := TeamCar{Name: name, Names: []string{}}
	err := p.pool.QueryRow(ctx, `INSERT INTO team_cars (team_id, name) VALUES ($1,$2) RETURNING id`, p.team, name).Scan(&c.ID)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == uniqueViolation {
		return TeamCar{}, ErrExists
	}
	return c, err
}

func (p *Postgres) SetLapExcluded(ctx context.Context, sessionID string, carID int, vehicleName string, lap int, excluded *bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE laps SET excluded = $6
WHERE team_id = $1 AND session_id = $2 AND car_id = $3 AND vehicle_name = $4 AND lap = $5`,
		p.team, sessionID, carID, vehicleName, lap, excluded)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoLap
	}
	return err
}

func (p *Postgres) SetSessionExcluded(ctx context.Context, sessionID string, excluded bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE sessions SET excluded = $3 WHERE team_id = $1 AND session_id = $2`, p.team, sessionID, excluded)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoSession
	}
	return err
}

func (p *Postgres) DeleteCar(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM team_cars WHERE team_id = $1 AND id = $2`, p.team, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoCar
	}
	return err
}

func (p *Postgres) MapCarName(ctx context.Context, id int64, vehicleName string) error {
	// The car is looked up with the team, so a name cannot be put on the car of another team.
	tag, err := p.pool.Exec(ctx, `
INSERT INTO team_car_names (team_id, vehicle_name, car_id)
SELECT $1, $3, c.id FROM team_cars c WHERE c.team_id = $1 AND c.id = $2
ON CONFLICT (team_id, vehicle_name) DO UPDATE SET car_id = EXCLUDED.car_id`, p.team, id, vehicleName)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoCar
	}
	return err
}

func (p *Postgres) UnmapCarName(ctx context.Context, vehicleName string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM team_car_names WHERE team_id = $1 AND vehicle_name = $2`, p.team, vehicleName)
	return err
}

func (p *Postgres) UnmappedCarNames(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
SELECT DISTINCT car FROM sessions
WHERE team_id = $1 AND car <> '' AND car NOT IN (SELECT vehicle_name FROM team_car_names WHERE team_id = $1)
ORDER BY car`, p.team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) TouchSession(ctx context.Context, s SessionInfo) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO sessions (session_id, track, session_type, car, car_id, started_at, last_seen, last_et, team_id)
VALUES ($1,$2,$3,$4,$7,$5,$5,$6,$8)
ON CONFLICT (team_id, session_id) DO UPDATE SET
    track = COALESCE(NULLIF(EXCLUDED.track, ''), sessions.track),
    session_type = COALESCE(NULLIF(EXCLUDED.session_type, ''), sessions.session_type),
    car = COALESCE(NULLIF(EXCLUDED.car, ''), sessions.car),
    car_id = CASE WHEN EXCLUDED.car_id >= 0 THEN EXCLUDED.car_id ELSE sessions.car_id END,
    last_seen = EXCLUDED.last_seen,
    last_et = EXCLUDED.last_et`,
		s.SessionID, s.Track, s.SessionType, s.Car, s.LastSeen, s.LastET, s.CarID, p.team)
	return err
}

func (p *Postgres) Sessions(ctx context.Context) ([]SessionSummary, error) {
	rows, err := p.pool.Query(ctx, `
SELECT s.session_id, s.track, s.session_type, s.car, s.car_id, s.started_at, s.last_seen, s.last_et, s.excluded,
  (SELECT count(*) FROM laps l WHERE l.team_id = s.team_id AND l.session_id = s.session_id AND l.vehicle_name = s.car AND (s.car_id < 0 OR l.car_id = s.car_id)),
  (SELECT count(*) FROM stops t WHERE t.team_id = s.team_id AND t.session_id = s.session_id AND t.vehicle_name = s.car AND (s.car_id < 0 OR t.car_id = s.car_id)),
  (SELECT count(*) FROM lap_conditions c WHERE c.team_id = s.team_id AND c.session_id = s.session_id),
  (SELECT count(*) FROM lap_conditions c WHERE c.team_id = s.team_id AND c.session_id = s.session_id AND c.control = 1),
  (SELECT count(*) FROM lap_conditions c WHERE c.team_id = s.team_id AND c.session_id = s.session_id AND c.control = 0)
FROM sessions s WHERE s.team_id = $1 ORDER BY s.last_seen DESC`, p.team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionSummary{}
	for rows.Next() {
		var s SessionSummary
		if err := rows.Scan(&s.SessionID, &s.Track, &s.SessionType, &s.Car, &s.CarID, &s.StartedAt, &s.LastSeen, &s.LastET, &s.Excluded,
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
  surface, inner_temp, pressure, brake, water, oil, compound_front, compound_rear, team_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (team_id, session_id, car_id, vehicle_name, lap) DO UPDATE SET
  et = EXCLUDED.et, control = EXCLUDED.control, ambient = EXCLUDED.ambient, track = EXCLUDED.track,
  rain = EXCLUDED.rain, wetness = EXCLUDED.wetness, surface = EXCLUDED.surface, inner_temp = EXCLUDED.inner_temp,
  pressure = EXCLUDED.pressure, brake = EXCLUDED.brake, water = EXCLUDED.water, oil = EXCLUDED.oil,
  compound_front = EXCLUDED.compound_front, compound_rear = EXCLUDED.compound_rear`,
		c.SessionID, c.CarID, c.VehicleName, c.Lap, c.ET, c.Control, c.Ambient, c.Track, c.Rain, c.Wetness,
		arr(c.Surface), arr(c.Inner), arr(c.Pressure), arr(c.Brake), c.Water, c.Oil, c.CompoundFront, c.CompoundRear, p.team)
	return err
}

func (p *Postgres) LapConditions(ctx context.Context, sessionID string) ([]LapConditions, error) {
	rows, err := p.pool.Query(ctx, `
SELECT car_id, vehicle_name, lap, et, control, ambient, track, rain, wetness, surface, inner_temp, pressure, brake,
  water, oil, compound_front, compound_rear
FROM lap_conditions WHERE team_id = $2 AND session_id = $1 ORDER BY car_id, lap`, sessionID, p.team)
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
INSERT INTO weather_points (session_id, et, ambient, track, rain, wetness, team_id) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (team_id, session_id, et) DO UPDATE SET ambient = EXCLUDED.ambient, track = EXCLUDED.track,
  rain = EXCLUDED.rain, wetness = EXCLUDED.wetness`, sessionID, w.ET, w.Ambient, w.Track, w.Rain, w.Wetness, p.team)
	return err
}

func (p *Postgres) WeatherPoints(ctx context.Context, sessionID string, limit int) ([]WeatherPoint, error) {
	rows, err := p.pool.Query(ctx, `
SELECT et, ambient, track, rain, wetness FROM (
  SELECT et, ambient, track, rain, wetness FROM weather_points WHERE team_id = $3 AND session_id = $1 ORDER BY et DESC LIMIT $2
) newest ORDER BY et`, sessionID, limit, p.team)
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

func (p *Postgres) TeamByName(ctx context.Context, name string) (Team, bool, error) {
	var t Team
	err := p.pool.QueryRow(ctx, `SELECT id, name FROM teams WHERE name = $1`, name).Scan(&t.ID, &t.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, false, nil
	}
	return t, err == nil, err
}

// uniqueViolation is the SQLSTATE of a duplicate key.
const uniqueViolation = "23505"

func (p *Postgres) CreateTeam(ctx context.Context, name string) (Team, error) {
	t := Team{Name: name}
	err := p.pool.QueryRow(ctx, `INSERT INTO teams (name) VALUES ($1) RETURNING id`, name).Scan(&t.ID)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == uniqueViolation {
		return Team{}, ErrExists
	}
	return t, err
}

func (p *Postgres) CreateAgent(ctx context.Context, name string, teamID int64, tokenHash []byte) (Agent, error) {
	a := Agent{Name: name, TeamID: teamID}
	err := p.pool.QueryRow(ctx, `INSERT INTO agents (name, token_hash, team_id) VALUES ($1,$2,$3) RETURNING id`,
		name, tokenHash, teamID).Scan(&a.ID)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == uniqueViolation {
		return Agent{}, ErrExists
	}
	if errors.As(err, &pe) && pe.Code == foreignKeyViolation {
		return Agent{}, ErrNoTeam
	}
	return a, err
}

func (p *Postgres) SetAgentEmail(ctx context.Context, id int64, email string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE agents SET email = $2 WHERE id = $1`, id, email)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoAgent
	}
	return err
}

func (p *Postgres) RenameAgent(ctx context.Context, id int64, name string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE agents SET name = $2 WHERE id = $1`, id, name)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNoAgent
	}
	return err
}

func (p *Postgres) RevokeAgent(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE agents SET revoked = true WHERE id = $1`, id)
	return err
}

func (p *Postgres) AgentByTokenHash(ctx context.Context, hash []byte) (Agent, bool, error) {
	var a Agent
	err := p.pool.QueryRow(ctx, `SELECT id, name, team_id, email, revoked, last_seen FROM agents WHERE token_hash = $1`, hash).
		Scan(&a.ID, &a.Name, &a.TeamID, &a.Email, &a.Revoked, &a.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, false, nil
	}
	return a, err == nil, err
}

func (p *Postgres) TouchAgent(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE agents SET last_seen = now() WHERE id = $1`, id)
	return err
}

func (p *Postgres) Teams(ctx context.Context) ([]Team, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name FROM teams ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const memberSelect = `SELECT m.email, m.team_id, t.name, m.is_admin, m.name, m.short, m.cf_client_id, m.cf_secret IS NOT NULL
FROM team_members m JOIN teams t ON t.id = m.team_id`

func (p *Postgres) MemberByEmail(ctx context.Context, email string) (Member, bool, error) {
	var v Member
	err := p.pool.QueryRow(ctx, memberSelect+` WHERE m.email = $1`, email).Scan(&v.Email, &v.TeamID, &v.Team, &v.IsAdmin, &v.Name, &v.Short, &v.CFClientID, &v.HasCFSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, false, nil
	}
	return v, err == nil, err
}

func (p *Postgres) Members(ctx context.Context) ([]Member, error) {
	rows, err := p.pool.Query(ctx, memberSelect+` ORDER BY m.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var v Member
		if err := rows.Scan(&v.Email, &v.TeamID, &v.Team, &v.IsAdmin, &v.Name, &v.Short, &v.CFClientID, &v.HasCFSecret); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// foreignKeyViolation is the SQLSTATE of a missing referenced row.
const foreignKeyViolation = "23503"

func (p *Postgres) SetMember(ctx context.Context, v Member) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO team_members (email, team_id, is_admin, name, short, cf_client_id) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (email) DO UPDATE SET team_id = EXCLUDED.team_id, is_admin = EXCLUDED.is_admin, name = EXCLUDED.name,
    short = EXCLUDED.short, cf_client_id = EXCLUDED.cf_client_id`,
		v.Email, v.TeamID, v.IsAdmin, v.Name, v.Short, v.CFClientID)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == foreignKeyViolation {
		return ErrNoTeam
	}
	if errors.As(err, &pe) && pe.Code == uniqueViolation {
		return ErrShortTaken
	}
	return err
}

func (p *Postgres) SetMemberSecret(ctx context.Context, email string, sealed []byte) error {
	_, err := p.pool.Exec(ctx, `UPDATE team_members SET cf_secret = $2 WHERE email = $1`, email, sealed)
	return err
}

func (p *Postgres) MemberSecret(ctx context.Context, email string) ([]byte, error) {
	var sealed []byte
	err := p.pool.QueryRow(ctx, `SELECT cf_secret FROM team_members WHERE email = $1`, email).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return sealed, err
}

func (p *Postgres) DeleteMember(ctx context.Context, email string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM team_members WHERE email = $1`, email)
	return err
}

func (p *Postgres) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name, team_id, email, revoked, last_seen FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.TeamID, &a.Email, &a.Revoked, &a.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) AddAudit(ctx context.Context, e AuditEntry) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO audit_log (ts, email, team, method, path) VALUES ($1,$2,$3,$4,$5)`,
		e.TS, e.Email, e.Team, e.Method, e.Path)
	return err
}

func (p *Postgres) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := p.pool.Query(ctx, `SELECT ts, email, team, method, path FROM audit_log ORDER BY ts DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.TS, &e.Email, &e.Team, &e.Method, &e.Path); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
