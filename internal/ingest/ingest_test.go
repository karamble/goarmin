// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

const serial = 3_500_000_001

// zone is the watch's time zone in these tests: UTC+1.
var zone = time.FixedZone("watch", 3600)

// day6 is 06:00 local time on today's date in the watch's zone: every test time is an offset
// from it, so the whole test day lies within one local day.
func day6() time.Time {
	n := time.Now().In(zone)
	return time.Date(n.Year(), n.Month(), n.Day(), 6, 0, 0, 0, zone)
}

func ft(t time.Time) proto.Value { return proto.Uint32(fittest.FITTime(t)) }

type F = fittest.F

// monitoringFile is a FIT 32 file: the zone, steps for two activity types, heart rate on
// timestamp_16, stress, Body Battery and respiration.
func monitoringFile(base time.Time) []byte { return monitoringFileOf(serial, base) }

// monitoringFileOf is monitoringFile written by the watch with serial number sn.
func monitoringFileOf(sn uint32, base time.Time) []byte {
	m := []proto.Message{
		fittest.Message(103, F{Num: 253, Value: ft(base)}, F{Num: 0, Value: ft(base.Add(time.Hour))}),
	}
	for i, st := range []struct {
		act   uint8
		steps uint32
	}{{6, 100}, {1, 30}, {6, 250}, {1, 80}} {
		at := base.Add(time.Duration(i+1) * 15 * time.Minute)
		m = append(m, fittest.Message(55, F{Num: 253, Value: ft(at)}, F{Num: 5, Value: proto.Uint8(st.act)},
			F{Num: 3, Value: proto.Uint32(st.steps)}))
		hrAt := at.Add(5 * time.Minute)
		m = append(m, fittest.Message(55, F{Num: 26, Value: proto.Uint16(uint16(fittest.FITTime(hrAt)))},
			F{Num: 27, Value: proto.Uint8(uint8(60 + 10*i))}))
		m = append(m, fittest.Message(227, F{Num: 1, Value: ft(at)}, F{Num: 0, Value: proto.Int16(int16(20 + 10*i))},
			F{Num: 3, Value: proto.Int8(int8(50 - i))}))
		m = append(m, fittest.Message(297, F{Num: 253, Value: ft(at)}, F{Num: 0, Value: proto.Int16(1500)}))
	}
	// Invalid readings are skipped: stress -1, respiration -200.
	m = append(m, fittest.Message(227, F{Num: 1, Value: ft(base.Add(2 * time.Hour))}, F{Num: 0, Value: proto.Int16(-1)}))
	m = append(m, fittest.Message(297, F{Num: 253, Value: ft(base.Add(2 * time.Hour))}, F{Num: 0, Value: proto.Int16(-200)}))
	return fittest.Build(typedef.FileMonitoringB, sn, base.Add(2*time.Hour), m...)
}

// sleepFile is a FIT 72 file with one night that ended at wake.
func sleepFile(start, wake time.Time) []byte {
	return fittest.Build(typedef.File(72), serial, wake.Add(time.Hour),
		fittest.Message(384, F{Num: 253, Value: ft(wake)}, F{Num: 9, Value: ft(start)}, F{Num: 11, Value: ft(wake)},
			F{Num: 10, Value: proto.Int16(60)}, F{Num: 12, Value: proto.Int16(60)}, F{Num: 2, Value: proto.Uint8(81)},
			F{Num: 0, Value: proto.Uint8(52)}, F{Num: 14, Value: proto.Uint8(30)}, F{Num: 16, Value: proto.Uint8(88)}),
		fittest.Message(411, F{Num: 10, Value: ft(start)}, F{Num: 8, Value: ft(wake)}, F{Num: 0, Value: proto.Uint8(81)},
			F{Num: 1, Value: proto.Uint16(60)}, F{Num: 2, Value: proto.Uint16(240)}, F{Num: 3, Value: proto.Uint16(90)},
			F{Num: 4, Value: proto.Uint16(10)}, F{Num: 5, Value: proto.Uint16(0)}),
	)
}

func newIngester(t *testing.T) *Ingester {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blobstore.Open(filepath.Join(dir, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	return &Ingester{Store: st, Blobs: blobs}
}

func write(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportIngestRollup(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, err := in.Store.EnsureUser(ctx, "a@example.invalid", "A")
	if err != nil {
		t.Fatal(err)
	}
	base := day6()
	dir := t.TempDir()
	write(t, dir, "Monitor/M1.FIT", monitoringFile(base))
	write(t, dir, "Sleep/S1.fit", sleepFile(base.Add(-7*time.Hour), base.Add(30*time.Minute)))
	write(t, dir, "notes.txt", []byte("not a fit file"))

	rep, err := in.Import(ctx, u, store.SourceUSB, []string{dir})
	if err != nil || rep.Seen != 2 || rep.New != 2 || rep.Ingested != 2 || rep.Failed != 0 {
		t.Fatalf("import %+v, %v", rep, err)
	}
	day := base.Format(time.DateOnly)
	got := summary(t, in, u.ID, day)
	want := map[string]int64{
		"steps": 250 + 80, "hr_min": 60, "hr_max": 90, "hr_avg": 75, "stress_avg": 35, "stress_max": 50,
		"bb_min": 47, "bb_max": 50, "respiration_avg": 1500, "sleep_score": 81, "sleep_s": int64(7*time.Hour/time.Second + 30*60),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary of %s:\n got %v\nwant %v", day, got, want)
	}
	sl, err := in.Store.SleepSessions(ctx, u.ID, day, day)
	if err != nil || len(sl) != 1 {
		t.Fatalf("sleep %+v, %v", sl, err)
	}
	if s := sl[0]; *s.Deep != 3600 || *s.REM != 90*60 || *s.BBStart != 30 || *s.BBEnd != 88 || *s.MinHR != 52 {
		t.Fatalf("sleep session %+v", s)
	}

	// Importing again stores nothing; ingesting everything again changes nothing.
	rep, err = in.Import(ctx, u, store.SourceUSB, []string{dir})
	if err != nil || rep.New != 0 || rep.Known != 2 {
		t.Fatalf("second import %+v, %v", rep, err)
	}
	if err := in.Store.ResetIngest(ctx, Handler); err != nil {
		t.Fatal(err)
	}
	if done, failed, err := in.Pending(ctx); err != nil || done != 2 || failed != 0 {
		t.Fatalf("reingest %d/%d, %v", done, failed, err)
	}
	if again := summary(t, in, u.ID, day); !reflect.DeepEqual(again, want) {
		t.Fatalf("after reingest %v", again)
	}
}

// summary returns the non-empty columns of one daily summary.
func summary(t *testing.T, in *Ingester, user int64, day string) map[string]int64 {
	t.Helper()
	ds, err := in.Store.DaySummaries(context.Background(), user, day, day)
	if err != nil || len(ds) != 1 {
		t.Fatalf("summaries %+v, %v", ds, err)
	}
	out := map[string]int64{}
	d := ds[0]
	for name, p := range map[string]*int64{"steps": d.Steps, "hr_min": d.HRMin, "hr_max": d.HRMax, "hr_avg": d.HRAvg,
		"stress_avg": d.StressAvg, "stress_max": d.StressMax, "bb_min": d.BBMin, "bb_max": d.BBMax,
		"respiration_avg": d.RespirationAvg, "sleep_score": d.SleepScore, "sleep_s": d.Sleep, "resting_hr": d.RestingHR} {
		if p != nil {
			out[name] = *p
		}
	}
	return out
}

// TestDuplicateRecordings: the same recording with other bytes is a duplicate, and a copy
// from the watch replaces an exported one.
func TestDuplicateRecordings(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	base := day6()
	orig := monitoringFile(base)
	// The same file_id with one more message: other bytes, same recording.
	other := fittest.Build(typedef.FileMonitoringB, serial, base.Add(2*time.Hour),
		fittest.Message(227, F{Num: 1, Value: ft(base.Add(3 * time.Hour))}, F{Num: 0, Value: proto.Int16(99)}))
	dir := t.TempDir()
	exp := write(t, dir, "export/M1.fit", other)
	usb := write(t, dir, "usb/M1.fit", orig)

	if rep, err := in.Import(ctx, u, store.SourceGCExport, []string{exp}); err != nil || rep.New != 1 {
		t.Fatalf("export import %+v, %v", rep, err)
	}
	day := base.Format(time.DateOnly)
	if s := summary(t, in, u.ID, day); s["stress_max"] != 99 {
		t.Fatalf("export data not ingested: %v", s)
	}
	if rep, err := in.Import(ctx, u, store.SourceUSB, []string{usb}); err != nil || rep.New != 1 || rep.Ingested != 1 {
		t.Fatalf("usb import %+v, %v", rep, err)
	}
	if s := summary(t, in, u.ID, day); s["stress_max"] != 50 || s["steps"] != 330 {
		t.Fatalf("the USB copy did not replace the export: %v", s)
	}
	// A third copy from an export is now the duplicate.
	third := write(t, dir, "export2/M1.fit", fittest.Build(typedef.FileMonitoringB, serial, base.Add(2*time.Hour)))
	if rep, err := in.Import(ctx, u, store.SourceGCExport, []string{third}); err != nil || rep.Duplicates != 1 {
		t.Fatalf("second export %+v, %v", rep, err)
	}
}

// TestWatchOwnership: a watch belongs to the first account that imported its files.
func TestWatchOwnership(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	a, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	b, _ := in.Store.EnsureUser(ctx, "b@example.invalid", "")
	if !a.IsAdmin || b.IsAdmin {
		t.Fatalf("admin flags %v %v", a.IsAdmin, b.IsAdmin)
	}
	dir := t.TempDir()
	p := write(t, dir, "M1.fit", monitoringFile(day6()))
	if rep, err := in.Import(ctx, a, store.SourceUSB, []string{p}); err != nil || rep.Ingested != 1 {
		t.Fatalf("%+v, %v", rep, err)
	}
	q := write(t, dir, "M2.fit", monitoringFile(day6().Add(-24*time.Hour)))
	rep, err := in.Import(ctx, b, store.SourceUSB, []string{q})
	if err != nil || rep.Failed != 1 || len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "another account") {
		t.Fatalf("b imported a's watch: %+v, %v", rep, err)
	}
}

// TestHandOver: each file goes to whoever owned the watch when it was recorded, and a spool's
// files leave once stored, so nothing is read twice and deleted data has nowhere to return from.
func TestHandOver(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	a, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	b, _ := in.Store.EnsureUser(ctx, "b@example.invalid", "")
	now := time.Now()
	spool := t.TempDir()
	// monitoringFile(base) is written at base+2h (its time_created): before the hand-over ...
	before := write(t, spool, "FIT_TYPE_32/before.fit", monitoringFile(now.Add(-5*time.Hour)))
	dev, err := in.Store.EnsureDevice(ctx, store.Device{UnitID: serial})
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Store.ClaimDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	// a hands the watch over two hours ago; nobody has it yet.
	in.Store.SetClock(func() time.Time { return now.Add(-2 * time.Hour) })
	if err := in.Store.ReleaseDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	in.Store.SetClock(time.Now)
	// ... and after it.
	after := write(t, spool, "FIT_TYPE_32/after.fit", monitoringFile(now.Add(-3*time.Hour)))

	rep, err := in.ImportWatch(ctx, dev, store.SourceBLE, spool)
	if err != nil || rep.New != 1 || rep.Waiting != 1 {
		t.Fatalf("before b adds it: %+v, %v", rep, err)
	}
	if _, err := os.Stat(before); !os.IsNotExist(err) {
		t.Fatal("a stored file stayed in the spool")
	}
	if _, err := os.Stat(after); err != nil {
		t.Fatal("a waiting file left the spool")
	}
	// A USB copy of a later file is refused with a clear reason while nobody owns the watch.
	usb := write(t, t.TempDir(), "M.fit", monitoringFile(now.Add(-30*time.Minute)))
	if rep, _ := in.Import(ctx, b, store.SourceUSB, []string{usb}); rep.Failed != 1 || !strings.Contains(rep.Errors[0], "handed over") {
		t.Fatalf("usb import of a handed-over watch: %+v", rep)
	}

	if err := in.Store.ClaimDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	rep, err = in.ImportWatch(ctx, dev, store.SourceBLE, spool)
	if err != nil || rep.New != 1 || rep.Waiting != 0 {
		t.Fatalf("after b added it: %+v, %v", rep, err)
	}
	owners := map[int64]int{}
	files, _ := in.Store.RawFilesOf(ctx, dev.ID)
	for _, f := range files {
		owners[f.UserID]++
	}
	if owners[a.ID] != 1 || owners[b.ID] != 1 {
		t.Fatalf("owners of the files %v (a=%d, b=%d)", owners, a.ID, b.ID)
	}
}

// activityFile is a one-session walk with a short GPS track.
func activityFile(start time.Time) []byte {
	var msgs []proto.Message
	for k := range 120 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp = start.Add(time.Duration(k) * time.Second)
		r.PositionLat, r.PositionLong = int32((50+float64(k)*1e-4)*(1<<31)/180), int32(7*(1<<31)/180)
		r.HeartRate = 100
		msgs = append(msgs, r.ToMesg(nil))
	}
	l := mesgdef.NewLap(nil)
	l.StartTime, l.Timestamp, l.TotalTimerTime = start, start.Add(2*time.Minute), 120000
	s := mesgdef.NewSession(nil)
	s.StartTime, s.Timestamp, s.Sport = start, start.Add(2*time.Minute), typedef.SportWalking
	s.TotalTimerTime, s.TotalElapsedTime, s.TotalDistance, s.AvgHeartRate = 120000, 120000, 15000, 100
	msgs = append(msgs, l.ToMesg(nil), s.ToMesg(nil))
	return fittest.Build(typedef.FileActivity, serial, start.Add(2*time.Minute), msgs...)
}

// TestActivities: an activity file becomes an activity with laps and a track; ingesting it
// again keeps its id; another account cannot see it.
func TestActivities(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	a, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	b, _ := in.Store.EnsureUser(ctx, "b@example.invalid", "")
	start := day6().Add(3 * time.Hour)
	p := write(t, t.TempDir(), "Activity/A1.fit", activityFile(start))
	if rep, err := in.Import(ctx, a, store.SourceUSB, []string{p}); err != nil || rep.Ingested != 1 {
		t.Fatalf("%+v, %v", rep, err)
	}
	acts, err := in.Store.Activities(ctx, a.ID, 0, 10)
	if err != nil || len(acts) != 1 {
		t.Fatalf("activities %+v, %v", acts, err)
	}
	act := acts[0]
	if act.Start != start.Unix() || act.Day != start.Format(time.DateOnly) || *act.Distance != 150 || act.Polyline == "" {
		t.Fatalf("activity %+v", act)
	}
	laps, err := in.Store.Laps(ctx, a.ID, act.ID)
	if err != nil || len(laps) != 1 {
		t.Fatalf("laps %+v, %v", laps, err)
	}
	fields, data, err := in.Store.Stream(ctx, a.ID, act.ID)
	if err != nil || len(data) == 0 || !reflect.DeepEqual(fields, []string{"lat", "lon", "hr"}) {
		t.Fatalf("stream %v %d bytes, %v", fields, len(data), err)
	}
	if _, err := in.Store.Activity(ctx, b.ID, act.ID); err != store.ErrNotFound {
		t.Fatalf("another account read the activity: %v", err)
	}
	if _, _, err := in.Store.Stream(ctx, b.ID, act.ID); err != store.ErrNotFound {
		t.Fatalf("another account read the track: %v", err)
	}
	if err := in.Store.ResetIngest(ctx, Handler); err != nil {
		t.Fatal(err)
	}
	if _, failed, err := in.Pending(ctx); err != nil || failed != 0 {
		t.Fatalf("reingest: %v", err)
	}
	again, _ := in.Store.Activities(ctx, a.ID, 0, 10)
	if len(again) != 1 || again[0].ID != act.ID {
		t.Fatalf("after reingest %+v (id was %d)", again, act.ID)
	}
	if laps, _ := in.Store.Laps(ctx, a.ID, act.ID); len(laps) != 1 {
		t.Fatalf("laps after reingest: %d", len(laps))
	}
}

// TestDeleteDeviceData: deleting what came from one watch leaves the other watch's data whole
// (rows the two shared are restored from it), removes days nothing is left for, and never
// touches another account.
func TestDeleteDeviceData(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	a, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	b, _ := in.Store.EnsureUser(ctx, "b@example.invalid", "")
	const other = 3_500_000_002
	today := day6()
	dir := t.TempDir()
	// a wore both watches on the same day (the same timestamps); only the first one the day
	// before, with a walk and a night.
	// The other watch first: the deleted one's import then owns the rows they share.
	files := []string{
		write(t, dir, "y/today.fit", monitoringFileOf(other, today)),
		write(t, dir, "x/today.fit", monitoringFile(today)),
		write(t, dir, "x/yesterday.fit", monitoringFile(today.Add(-24*time.Hour))),
		write(t, dir, "x/walk.fit", activityFile(today.Add(-21*time.Hour))),
		write(t, dir, "x/night.fit", sleepFile(today.Add(-30*time.Hour), today.Add(-23*time.Hour))),
	}
	if rep, err := in.Import(ctx, a, store.SourceUSB, files); err != nil || rep.Failed != 0 {
		t.Fatalf("import %+v, %v", rep, err)
	}
	// b has a watch of their own.
	if rep, err := in.Import(ctx, b, store.SourceUSB, []string{write(t, t.TempDir(), "z.fit", monitoringFileOf(3_500_000_003, today))}); err != nil || rep.Ingested != 1 {
		t.Fatalf("b's import %+v, %v", rep, err)
	}
	x, _ := in.Store.DeviceByUnit(ctx, serial)
	if err := in.Store.RecordSyncRun(ctx, store.SyncRun{DeviceID: x.ID, Started: time.Now().Add(-time.Minute), Finished: time.Now(), OK: true}); err != nil {
		t.Fatal(err)
	}
	day, yesterday := today.Format(time.DateOnly), today.Add(-24*time.Hour).Format(time.DateOnly)
	before := summary(t, in, a.ID, day)
	xFiles, _ := in.Store.RawFilesOf(ctx, x.ID)

	gone, err := in.DeleteDeviceData(ctx, a.ID, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Files != 4 || gone.Activities != 1 || gone.Nights != 1 || gone.Runs != 1 {
		t.Fatalf("deleted %+v", gone)
	}
	if left, _ := in.Store.RawFilesOf(ctx, x.ID); len(left) != 0 {
		t.Fatalf("files of the watch left: %+v", left)
	}
	for _, f := range xFiles {
		if in.Blobs.Has(f.SHA256) {
			t.Fatalf("blob %s stayed", f.SHA256)
		}
	}
	// Today still has the other watch's data, as complete as it was (its samples restored).
	if after := summary(t, in, a.ID, day); after["steps"] != before["steps"] || after["hr_max"] != before["hr_max"] {
		t.Fatalf("today before %v, after %v", before, after)
	}
	// Yesterday had only the deleted watch's data: no day left.
	if ds, _ := in.Store.DaySummaries(ctx, a.ID, yesterday, yesterday); len(ds) != 0 {
		t.Fatalf("yesterday still there: %+v", ds)
	}
	if acts, _ := in.Store.Activities(ctx, a.ID, 0, 10); len(acts) != 0 {
		t.Fatalf("activities left %+v", acts)
	}
	if runs, _ := in.Store.RecentSyncRuns(ctx, x.ID, 10); len(runs) != 0 {
		t.Fatalf("sync runs left %+v", runs)
	}
	// b is untouched.
	if s := summary(t, in, b.ID, day); s["steps"] == 0 {
		t.Fatalf("b lost data: %v", s)
	}
}

// TestActivityPeriodsAndNeighbours: activities by local day range, and the one before and
// after an activity.
func TestActivityPeriodsAndNeighbours(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	a, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	base := day6().Add(3 * time.Hour)
	dir := t.TempDir()
	var files []string
	for i := range 3 {
		files = append(files, write(t, dir, fmt.Sprintf("Activity/A%d.fit", i), activityFile(base.Add(time.Duration(i)*24*time.Hour))))
	}
	if rep, err := in.Import(ctx, a, store.SourceUSB, files); err != nil || rep.Ingested != 3 {
		t.Fatalf("%+v, %v", rep, err)
	}
	first, mid := base.Format(time.DateOnly), base.Add(24*time.Hour).Format(time.DateOnly)
	got, err := in.Store.ActivitiesIn(ctx, a.ID, first, mid)
	if err != nil || len(got) != 2 || got[0].Day != mid || got[1].Day != first {
		t.Fatalf("days %s..%s: %+v, %v", first, mid, got, err)
	}
	next, err := in.Store.ActivityAfter(ctx, a.ID, got[1].Start)
	if err != nil || next.ID != got[0].ID {
		t.Fatalf("after the first: %+v, %v", next, err)
	}
	all, _ := in.Store.Activities(ctx, a.ID, 0, 10)
	if _, err := in.Store.ActivityAfter(ctx, a.ID, all[0].Start); err != store.ErrNotFound {
		t.Fatalf("after the newest: %v", err)
	}
}

// TestReingestDropsDaysTheFileLeft: a file that once wrote a day (a decoder bug put samples on
// it) and no longer does after a reingest takes that day's summary with it.
func TestReingestDropsDaysTheFileLeft(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, err := in.Store.EnsureUser(ctx, "a@example.invalid", "A")
	if err != nil {
		t.Fatal(err)
	}
	base := day6()
	b := monitoringFile(base)
	dir := t.TempDir()
	write(t, dir, "Monitor/M1.FIT", b)
	if _, err := in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	f, err := in.Store.RawFileBySHA(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	// As an older decoder did: one sample two days off, from the same file.
	stray := base.Add(-48 * time.Hour)
	days, err := in.Store.WriteFileData(ctx, f, &wellness.Data{Samples: []wellness.Sample{{Kind: wellness.Steps, At: stray, Sub: 6, Value: 999}}}, nil, Handler, Version-1)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Store.RebuildDays(ctx, u.ID, days); err != nil {
		t.Fatal(err)
	}
	strayDay := stray.In(time.Local).Format(time.DateOnly)
	if got := summary(t, in, u.ID, strayDay)["steps"]; got != 999 {
		t.Fatalf("stray day steps %d", got)
	}
	if err := in.File(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if ds, err := in.Store.DaySummaries(ctx, u.ID, strayDay, strayDay); err != nil || len(ds) != 0 {
		t.Fatalf("the stray day stayed: %+v, %v", ds, err)
	}
	if got := summary(t, in, u.ID, base.Format(time.DateOnly))["steps"]; got == 0 {
		t.Fatal("the file's own day lost its steps")
	}
}

// TestRebuildAllDays drops a summary whose day has no data left and keeps the others.
func TestRebuildAllDays(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, err := in.Store.EnsureUser(ctx, "a@example.invalid", "A")
	if err != nil {
		t.Fatal(err)
	}
	base := day6()
	dir := t.TempDir()
	write(t, dir, "Monitor/M1.FIT", monitoringFile(base))
	if _, err := in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil {
		t.Fatal(err)
	}
	// A summary left from data that is gone: a stray sample's day is summed, then the sample
	// goes without the day being rebuilt (as before the fileDays fix).
	stray := base.Add(-72 * time.Hour)
	empty := stray.In(time.Local).Format(time.DateOnly)
	f, _, err := in.Store.AddRawFile(ctx, store.RawFile{SHA256: strings.Repeat("ab", 32), UserID: u.ID, Source: store.SourceUSB, SourceName: "stray"})
	if err != nil {
		t.Fatal(err)
	}
	days, err := in.Store.WriteFileData(ctx, f, &wellness.Data{Samples: []wellness.Sample{{Kind: wellness.Steps, At: stray, Sub: 6, Value: 5}}}, nil, Handler, Version)
	if err != nil || in.Store.RebuildDays(ctx, u.ID, days) != nil {
		t.Fatal(err)
	}
	if _, err := in.Store.WriteFileData(ctx, f, &wellness.Data{}, nil, Handler, Version); err != nil {
		t.Fatal(err)
	}
	if ds, _ := in.Store.DaySummaries(ctx, u.ID, empty, empty); len(ds) != 1 {
		t.Fatalf("no leftover to test with: %+v", ds)
	}
	n, err := in.Store.RebuildAllDays(ctx)
	if err != nil || n == 0 {
		t.Fatalf("rebuilt %d, %v", n, err)
	}
	if ds, _ := in.Store.DaySummaries(ctx, u.ID, empty, empty); len(ds) != 0 {
		t.Fatalf("empty day kept: %+v", ds)
	}
	if summary(t, in, u.ID, base.Format(time.DateOnly))["steps"] == 0 {
		t.Fatal("the day with data lost its steps")
	}
}

// TestWatchFiles: the watch's settings, goals, totals, a workout, a course and an activity's
// own time in zones land in their tables for the file's owner, and go with the watch's data.
func TestWatchFiles(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, err := in.Store.EnsureUser(ctx, "a@example.invalid", "A")
	if err != nil {
		t.Fatal(err)
	}
	const unit = 3_600_000_042
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()

	dev := mesgdef.NewDeviceSettings(nil)
	dev.TimeMode = []typedef.TimeMode{typedef.TimeModeHour12}
	zt := mesgdef.NewZonesTarget(nil)
	zt.MaxHeartRate, zt.ThresholdHeartRate = 183, 161
	settings := []proto.Message{dev.ToMesg(nil), zt.ToMesg(nil)}
	for i, v := range []uint8{92, 110, 128, 146, 165, 183} {
		z := mesgdef.NewHrZone(nil)
		z.MessageIndex, z.HighBpm = typedef.MessageIndex(i), v
		settings = append(settings, z.ToMesg(nil))
	}
	write(t, dir, "Settings/Settings.fit", fittest.Build(typedef.FileSettings, unit, now.Add(-time.Hour), settings...))
	g := mesgdef.NewGoal(nil)
	g.Type, g.TargetValue, g.Source = typedef.GoalActiveMinutes, 150, typedef.GoalSourceUser
	write(t, dir, "Goals/Goals.fit", fittest.Build(typedef.FileGoals, unit, now.Add(-time.Hour), g.ToMesg(nil)))
	tot := mesgdef.NewTotals(nil)
	tot.Sport, tot.TimerTime, tot.Distance, tot.Calories, tot.Sessions = typedef.SportRunning, 6155, 11944, 1149, 3
	write(t, dir, "Totals/Totals.fit", fittest.Build(typedef.FileTotals, unit, now.Add(-time.Hour), tot.ToMesg(nil)))
	w := mesgdef.NewWorkout(nil)
	w.WktName, w.Sport = "Easy", typedef.SportRunning
	st := mesgdef.NewWorkoutStep(nil)
	st.MessageIndex, st.DurationType, st.DurationValue, st.Intensity = 0, typedef.WktStepDurationTime, 1200000, typedef.IntensityActive
	write(t, dir, "Workouts/Easy.fit", fittest.Build(typedef.FileWorkout, unit, now.Add(-time.Hour), w.ToMesg(nil), st.ToMesg(nil)))
	c := mesgdef.NewCourse(nil)
	c.Name, c.Sport = "Loop", typedef.SportRunning
	course := []proto.Message{c.ToMesg(nil)}
	for i := range 20 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp, r.Distance = now.Add(time.Duration(i)*time.Second), uint32(i*500)
		r.PositionLat, r.PositionLong = int32(536870912+i*1000), int32(83521000+i*500)
		course = append(course, r.ToMesg(nil))
	}
	write(t, dir, "Courses/Loop.fit", fittest.Build(typedef.FileCourse, unit, now.Add(-time.Hour), course...))
	// An activity with the watch's time in zones.
	start := now.Add(-3 * time.Hour)
	act := []proto.Message{}
	for i := range 60 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp, r.HeartRate = start.Add(time.Duration(i)*time.Second), 140
		act = append(act, r.ToMesg(nil))
	}
	ses := mesgdef.NewSession(nil)
	ses.MessageIndex, ses.StartTime, ses.Timestamp, ses.TotalTimerTime, ses.Sport = 0, start, start.Add(time.Minute), 60000, typedef.SportRunning
	tiz := mesgdef.NewTimeInZone(nil)
	tiz.ReferenceMesg, tiz.ReferenceIndex, tiz.Timestamp = typedef.MesgNumSession, 0, start.Add(time.Minute)
	tiz.HrZoneHighBoundary, tiz.TimeInHrZone = []uint8{92, 110, 128, 146, 165, 183}, []uint32{0, 0, 0, 60000, 0, 0, 0}
	write(t, dir, "Activity/A.fit", fittest.Build(typedef.FileActivity, unit, start, append(act, ses.ToMesg(nil), tiz.ToMesg(nil))...))

	rep, err := in.Import(ctx, u, store.SourceUSB, []string{dir})
	if err != nil || rep.Failed != 0 || rep.Ingested != 6 {
		t.Fatalf("import %+v, %v", rep, err)
	}
	ws, err := in.Store.LatestWatchSettings(ctx, u.ID)
	if err != nil || ws.Settings.Clock != "12h" || len(ws.Settings.Zones) != 1 || ws.Settings.Zones[0].HR[5] != 183 {
		t.Fatalf("settings %+v, %v", ws, err)
	}
	goals, _ := in.Store.WatchGoals(ctx, u.ID)
	totals, _ := in.Store.LifetimeTotals(ctx, u.ID)
	workouts, _ := in.Store.Workouts(ctx, u.ID, 0)
	courses, _ := in.Store.Courses(ctx, u.ID, 0)
	if len(goals) != 1 || goals[0].Target != 150 || len(totals) != 1 || totals[0].Sessions != 3 || len(workouts) != 1 ||
		workouts[0].EstimatedS != 1200 || len(courses) != 1 || courses[0].Polyline == "" || courses[0].DistanceM != 95 {
		t.Fatalf("goals %+v totals %+v workouts %+v courses %+v", goals, totals, workouts, courses)
	}
	if _, data, err := in.Store.CourseStream(ctx, u.ID, courses[0].ID); err != nil || len(data) == 0 {
		t.Fatalf("course track: %v", err)
	}
	acts, _ := in.Store.ActivitiesIn(ctx, u.ID, start.In(time.Local).Format(time.DateOnly), now.Format(time.DateOnly))
	if len(acts) != 1 {
		t.Fatalf("%d activities", len(acts))
	}
	zs, err := in.Store.ActivityZones(ctx, u.ID, acts[0].ID)
	if err != nil || !strings.Contains(string(zs[-1]), `"hr_s":[0,0,0,60,0,0,0]`) {
		t.Fatalf("zones %s, %v", zs[-1], err)
	}
	// Another account sees none of it.
	b, _ := in.Store.EnsureUser(ctx, "b@example.invalid", "B")
	if w, _ := in.Store.Workouts(ctx, b.ID, 0); len(w) != 0 {
		t.Fatal("another account sees the workouts")
	}
	if _, err := in.Store.Courses(ctx, b.ID, courses[0].ID); err == nil {
		t.Fatal("another account opens the course")
	}
	// Deleting the watch's data takes all of it.
	d, _ := in.Store.DeviceByUnit(ctx, unit)
	if _, err := in.DeleteDeviceData(ctx, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Store.LatestWatchSettings(ctx, u.ID); err == nil {
		t.Fatal("settings survived")
	}
	if w, _ := in.Store.Workouts(ctx, u.ID, 0); len(w) != 0 {
		t.Fatal("workouts survived")
	}
	if c, _ := in.Store.Courses(ctx, u.ID, 0); len(c) != 0 {
		t.Fatal("courses survived")
	}
}
