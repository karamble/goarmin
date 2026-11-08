// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func rawFile(t *testing.T, st *Store, user int64, name string, typ uint8, created time.Time) RawFile {
	t.Helper()
	sum := sha256.Sum256([]byte(fmt.Sprint(name, typ)))
	f, _, err := st.AddRawFile(context.Background(), RawFile{SHA256: hex.EncodeToString(sum[:]), Size: 1, UserID: user,
		Source: SourceUSB, SourceName: name, FIT: &FITIdentity{Type: typ, Manufacturer: 1, Serial: 7, TimeCreated: created.Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func unknownSleep(start, end time.Time) wellness.Sleep {
	u := wellness.Unknown
	return wellness.Sleep{Start: start, End: end, Score: u, QualityScore: u, DurationScore: u, RecoveryScore: u, MinHR: u,
		BodyBatteryStart: u, BodyBatteryEnd: u, Deep: u, Light: u, REM: u, Awake: u, Unmeasurable: u}
}

func nights(t *testing.T, st *Store, user int64) []SleepSession {
	t.Helper()
	list, err := st.SleepSessions(context.Background(), user, "0000-01-01", "9999-12-31")
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// TestNightsMerged: a sleep file and the daily-health file describe the same sleep, starting a
// few minutes apart. One night comes of them: the sleep file's bounds and stages, the
// daily-health file's Body Battery and lowest heart rate, the score from either.
func TestNightsMerged(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	wake := time.Now().Add(-3 * time.Hour).Truncate(time.Minute)
	bed := wake.Add(-7 * time.Hour)

	daily := unknownSleep(bed.Add(-4*time.Minute), wake.Add(time.Minute))
	daily.Score, daily.MinHR, daily.BodyBatteryStart, daily.BodyBatteryEnd = 79, 48, 20, 75
	daily.Deep, daily.Light, daily.REM, daily.Awake, daily.Unmeasurable = 3000, 15000, 4000, 3000, 0
	f72 := rawFile(t, st, u.ID, "daily", 72, wake.Add(time.Hour))
	if _, err := st.WriteFileData(ctx, f72, &wellness.Data{Sleep: []wellness.Sleep{daily}}, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	stages := unknownSleep(bed, wake)
	stages.Stages, stages.Score, stages.QualityScore = true, 79, 82
	stages.Deep, stages.Light, stages.REM, stages.Awake, stages.Unmeasurable = 5460, 14520, 3540, 1140, 0
	f49 := rawFile(t, st, u.ID, "sleep", 49, wake.Add(2*time.Hour))
	days, err := st.WriteFileData(ctx, f49, &wellness.Data{Sleep: []wellness.Sleep{stages},
		Naps: []wellness.Nap{{Start: wake.Add(time.Hour), End: wake.Add(80 * time.Minute), Feedback: 2, Source: 0}}}, nil, "fit", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) == 0 {
		t.Fatal("no days touched")
	}
	got := nights(t, st, u.ID)
	if len(got) != 1 {
		t.Fatalf("%d nights, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.Start != bed.Unix() || n.End != wake.Unix() || *n.Deep != 5460 || *n.Light != 14520 || *n.BBStart != 20 || *n.BBEnd != 75 ||
		*n.MinHR != 48 || *n.Score != 79 || *n.QualityScore != 82 {
		t.Fatalf("night %+v", n)
	}
	// Ingesting a file again leaves one night, and the night stays the same.
	if _, err := st.WriteFileData(ctx, f72, &wellness.Data{Sleep: []wellness.Sleep{daily}}, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	if got := nights(t, st, u.ID); len(got) != 1 || got[0].Start != bed.Unix() || *got[0].Deep != 5460 {
		t.Fatalf("after a reingest %+v", got)
	}
	// Without the sleep file, the night is the daily-health file's again.
	err = st.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := deleteFileData(ctx, tx, f49.ID); err != nil {
			return err
		}
		z, err := st.zone(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		return rebuildNights(ctx, tx, u.ID, bed.Unix(), wake.Unix(), z, map[string]bool{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := nights(t, st, u.ID); len(got) != 1 || got[0].Start != daily.Start.Unix() || *got[0].Deep != 3000 || got[0].QualityScore != nil {
		t.Fatalf("after the sleep file went %+v", got)
	}
	naps, err := st.Naps(ctx, u.ID, "0000-01-01", "9999-12-31")
	if err != nil || len(naps) != 0 {
		t.Fatalf("the sleep file's nap stayed: %+v %v", naps, err)
	}
}

// TestNightsApart: two sleeps that do not overlap stay two nights; a nap is kept apart.
func TestNightsApart(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	wake := time.Now().Add(-26 * time.Hour).Truncate(time.Minute)
	first := unknownSleep(wake.Add(-8*time.Hour), wake)
	second := unknownSleep(wake.Add(16*time.Hour), wake.Add(24*time.Hour))
	f := rawFile(t, st, u.ID, "daily", 72, wake.Add(25*time.Hour))
	d := &wellness.Data{Sleep: []wellness.Sleep{first, second}, Naps: []wellness.Nap{{Start: wake.Add(6 * time.Hour),
		End: wake.Add(7 * time.Hour), Feedback: wellness.Unknown, Source: 1}}}
	if _, err := st.WriteFileData(ctx, f, d, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	if got := nights(t, st, u.ID); len(got) != 2 {
		t.Fatalf("%d nights, want 2", len(got))
	}
	naps, err := st.Naps(ctx, u.ID, "0000-01-01", "9999-12-31")
	if err != nil || len(naps) != 1 || naps[0].Feedback != nil || naps[0].How == nil || *naps[0].How != 1 {
		t.Fatalf("naps %+v %v", naps, err)
	}
}

// TestLegacyNightsBecomeParts: a night stored before parts existed is a part of its file, so a
// later file's part of the same sleep merges with it instead of doubling it.
func TestLegacyNightsBecomeParts(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	wake := time.Now().Add(-5 * time.Hour).Truncate(time.Minute)
	old := rawFile(t, st, u.ID, "old", 72, wake)
	if _, err := st.db.W.ExecContext(ctx, `INSERT INTO sleep_parts (user_id, raw_file_id, source, prio, start_ts, end_ts, score)
		VALUES (?, ?, 'legacy', 2, ?, ?, 70)`, u.ID, old.ID, wake.Add(-7*time.Hour).Unix(), wake.Unix()); err != nil {
		t.Fatal(err)
	}
	s := unknownSleep(wake.Add(-7*time.Hour+3*time.Minute), wake)
	s.Stages, s.Deep, s.Light, s.REM, s.Awake, s.Unmeasurable = true, 1, 2, 3, 4, 0
	if _, err := st.WriteFileData(ctx, rawFile(t, st, u.ID, "sleep", 49, wake), &wellness.Data{Sleep: []wellness.Sleep{s}}, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	got := nights(t, st, u.ID)
	if len(got) != 1 || *got[0].Score != 70 || *got[0].Deep != 1 || got[0].Start != s.Start.Unix() {
		t.Fatalf("%+v", got)
	}
}

// TestMidnightTotals: the file that ends a day writes the day's total at local midnight, the
// file that starts the next day writes 0 at the same second. In either order, each day keeps
// its own total.
func TestMidnightTotals(t *testing.T) {
	ctx := context.Background()
	mid := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3) // a UTC midnight; the zone below is UTC
	offs := []wellness.Offset{{At: mid.Add(-12 * time.Hour), Offset: 0}}
	ending := &wellness.Data{Offsets: offs, Samples: []wellness.Sample{
		{Kind: wellness.Steps, At: mid.Add(-15 * time.Minute), Sub: 6, Value: 6000},
		{Kind: wellness.Steps, At: mid, Sub: 6, Value: 6107},
	}}
	starting := &wellness.Data{Offsets: offs, Samples: []wellness.Sample{
		{Kind: wellness.Steps, At: mid, Sub: 6, Value: 0},
		{Kind: wellness.Steps, At: mid.Add(15 * time.Minute), Sub: 6, Value: 50},
	}}
	for _, order := range [][2]*wellness.Data{{ending, starting}, {starting, ending}} {
		st := openTest(t)
		u := person(t, st, "a@example.invalid")
		var all []string
		for i, d := range order {
			// Each run decodes afresh: WriteFileData may move samples.
			cp := &wellness.Data{Offsets: d.Offsets, Samples: append([]wellness.Sample(nil), d.Samples...)}
			days, err := st.WriteFileData(ctx, rawFile(t, st, u.ID, fmt.Sprint("mon", i), 32, mid), cp, nil, "fit", 7)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, days...)
		}
		if err := st.RebuildDays(ctx, u.ID, all); err != nil {
			t.Fatal(err)
		}
		before, after := mid.AddDate(0, 0, -1).Format(time.DateOnly), mid.Format(time.DateOnly)
		sum, err := st.DaySummaries(ctx, u.ID, before, after)
		if err != nil || len(sum) != 2 || *sum[0].Steps != 6107 || *sum[1].Steps != 50 {
			t.Fatalf("days %+v %v", sum, err)
		}
	}
}

// TestZoneKeeps: which changes to the time-zone history leave the zone as it is, and the zone
// loaded from the change rows equals the one built from every row.
func TestZoneKeeps(t *testing.T) {
	now := time.Now().Unix()
	h := int64(3600)
	rows := map[int64]int64{now - 10*h: 0, now - 9*h: 0, now - 5*h: h, now - 4*h: h, now - 2*h: 0}
	z := NewZone(rows, time.UTC)
	for _, c := range []struct {
		name           string
		removed, added map[int64]int64
		want           bool
	}{
		{"nothing", nil, nil, true},
		{"an agreeing row", nil, map[int64]int64{now - 8*h: 0}, true},
		{"an agreeing UTC row after the last change", nil, map[int64]int64{now: 0}, true},
		{"a row inside a run goes", map[int64]int64{now - 9*h: 0}, nil, true},
		{"a change goes", map[int64]int64{now - 5*h: h}, nil, false},
		{"a disagreeing row", nil, map[int64]int64{now - 8*h: h}, false},
		{"a row before the history", nil, map[int64]int64{now - 20*h: 0}, false},
	} {
		if got := z.keeps(c.removed, c.added); got != c.want {
			t.Errorf("%s: keeps = %v, want %v", c.name, got, c.want)
		}
	}
	if (Zone{loc: time.UTC}).keeps(nil, map[int64]int64{now: 0}) {
		t.Error("the first row of a history changes the zone")
	}

	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	f := rawFile(t, st, u.ID, "z", 32, time.Unix(now, 0))
	for ts, o := range rows {
		if _, err := st.db.W.ExecContext(ctx, `INSERT INTO tz_offsets (user_id, ts, offset_s, raw_file_id) VALUES (?, ?, ?, ?)`, u.ID, ts, o, f.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.zone(ctx, st.db.R, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.ts, got.off) != fmt.Sprint(NewZone(rows, time.UTC).ts, NewZone(rows, time.UTC).off) {
		t.Fatalf("loaded %v %v, built %v %v", got.ts, got.off, z.ts, z.off)
	}
}
