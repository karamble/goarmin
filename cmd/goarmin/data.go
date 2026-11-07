// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// openData opens the server's database and blob store under dataDir.
func openData(ctx context.Context, dataDir string, log *slog.Logger) (*ingest.Ingester, func(), error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(ctx, filepath.Join(dataDir, "goarmin.db"))
	if err != nil {
		return nil, nil, err
	}
	blobs, err := blobstore.Open(filepath.Join(dataDir, "raw"))
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return &ingest.Ingester{Store: st, Blobs: blobs, Log: log}, func() { st.Close() }, nil
}

// lockBluetooth takes the data directory's Bluetooth lock for a command, or says why it can't:
// the server (goarmin serve --with-sync) holds it while it syncs, streams live values or pairs.
func lockBluetooth(name, dataDir string, stderr io.Writer) (func(), bool) {
	unlock, err := syncnode.LockBluetooth(dataDir)
	if errors.Is(err, syncnode.ErrBluetoothBusy) {
		fmt.Fprintf(stderr, "goarmin %s: the goarmin server (or another command) is using Bluetooth right now; try again in a minute, or stop it with make stop\n", name)
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(stderr, "goarmin %s: %v\n", name, err)
		return nil, false
	}
	return unlock, true
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory (database and raw files)")
	user := fs.String("user", config.String("GOARMIN_USER", ""), "email of the account the files belong to (created if new)")
	source := fs.String("source", "usb", "where the files come from: usb, ble (a sync spool), upload, gc_export")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin import -user EMAIL [-source usb|ble|upload|gc_export] PATH...\n\nStores every FIT file under the paths (walked) once, by content, and extracts its health\ndata. Importing the same files again changes nothing. PATH can be a USB copy of the watch's\nGARMIN folder or a sync spool (~/.local/share/goarmin/watches/<unit>/files, -source ble).")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *user == "" || fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	switch *source {
	case store.SourceUSB, store.SourceBLE, store.SourceUpload, store.SourceGCExport:
	default:
		fmt.Fprintf(stderr, "goarmin import: unknown source %q\n", *source)
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	in, closeData, err := openData(ctx, *dataDir, slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		fmt.Fprintln(stderr, "goarmin import:", err)
		return 1
	}
	defer closeData()
	u, err := in.Store.EnsureUser(ctx, *user, "")
	if err != nil {
		fmt.Fprintln(stderr, "goarmin import:", err)
		return 1
	}
	start := time.Now()
	rep, err := in.Import(ctx, u, *source, fs.Args())
	fmt.Fprintf(stdout, "import for %s: %d FIT files seen, %d new, %d already stored, %d duplicates of stored recordings, %d invalid; ingested %d, failed %d (%s)\n",
		u.Email, rep.Seen, rep.New, rep.Known, rep.Duplicates, rep.Invalid, rep.Ingested, rep.Failed, time.Since(start).Round(time.Millisecond))
	for _, e := range rep.Errors {
		fmt.Fprintln(stdout, "  !", e)
	}
	if err != nil {
		fmt.Fprintln(stderr, "goarmin import:", err)
		return 1
	}
	return 0
}

func runReingest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	all := fs.Bool("all", false, "ingest every file again, not only those an older extraction version read")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	in, closeData, err := openData(ctx, *dataDir, slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		fmt.Fprintln(stderr, "goarmin reingest:", err)
		return 1
	}
	defer closeData()
	if *all {
		if err := in.Store.ResetIngest(ctx, ingest.Handler); err != nil {
			fmt.Fprintln(stderr, "goarmin reingest:", err)
			return 1
		}
	}
	start := time.Now()
	done, failed, err := in.Pending(ctx)
	fmt.Fprintf(stdout, "reingest (%s v%d): %d files ingested, %d failed (%s)\n", ingest.Handler, ingest.Version, done, failed, time.Since(start).Round(time.Millisecond))
	if err != nil {
		fmt.Fprintln(stderr, "goarmin reingest:", err)
		return 1
	}
	// Every day again: a file that no longer reaches a day (a decoder fix) leaves nothing there.
	days, err := in.Store.RebuildAllDays(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin reingest:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d daily summaries rebuilt\n", days)
	return 0
}

func runDays(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("days", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	user := fs.String("user", config.String("GOARMIN_USER", ""), "email of the account")
	n := fs.Int("n", 14, "how many days back from today")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *user == "" {
		fmt.Fprintln(stderr, "usage: goarmin days -user EMAIL [-n DAYS]")
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	in, closeData, err := openData(ctx, *dataDir, nil)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin days:", err)
		return 1
	}
	defer closeData()
	u, err := in.Store.UserByEmail(ctx, *user)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin days:", err)
		return 1
	}
	now := time.Now()
	from, to := now.AddDate(0, 0, -*n+1).Format(time.DateOnly), now.Format(time.DateOnly)
	days, err := in.Store.DaySummaries(ctx, u.ID, from, to)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin days:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "day\tsteps\tkm\tactive kcal\tHR min\tavg\tmax\tresting\tstress avg\tBB min\tmax\tresp\tsleep\tscore\t")
	for _, d := range days {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", d.Day, i(d.Steps), scaled(d.DistanceCM, 1e-5, 1),
			i(d.ActiveKcal), i(d.HRMin), i(d.HRAvg), i(d.HRMax), i(d.RestingHR), i(d.StressAvg), i(d.BBMin), i(d.BBMax),
			scaled(d.RespirationAvg, 0.01, 1), hours(d.Sleep), i(d.SleepScore))
	}
	tw.Flush()
	if vals, err := in.Store.DailyValues(ctx, u.ID, from, to); err == nil && len(vals) > 0 {
		show := []wellness.Kind{wellness.StepGoal, wellness.RestingHR, wellness.SleepNeed, wellness.HRVLastNight,
			wellness.TrainingReadiness, wellness.VO2Max}
		byDay := map[string]map[wellness.Kind]int64{}
		var dayList []string
		for _, v := range vals {
			if byDay[v.Day] == nil {
				byDay[v.Day] = map[wellness.Kind]int64{}
				dayList = append(dayList, v.Day)
			}
			byDay[v.Day][v.Kind] = v.Value
		}
		fmt.Fprintln(stdout, "\ndaily values:")
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprint(tw, "day\t")
		for _, k := range show {
			fmt.Fprintf(tw, "%s\t", k)
		}
		fmt.Fprintln(tw)
		for _, day := range dayList {
			fmt.Fprintf(tw, "%s\t", day)
			for _, k := range show {
				v, ok := byDay[day][k]
				switch {
				case !ok:
					fmt.Fprint(tw, "-\t")
				case k == wellness.SleepNeed:
					fmt.Fprintf(tw, "%dh%02d\t", v/60, v%60)
				default:
					fmt.Fprintf(tw, "%g\t", float64(v)*k.Info().Scale)
				}
			}
			fmt.Fprintln(tw)
		}
		tw.Flush()
	}
	if sug, err := in.Store.SuggestedWorkouts(ctx, u.ID, to, now.AddDate(0, 0, 7).Format(time.DateOnly)); err == nil && len(sug) > 0 {
		fmt.Fprintln(stdout, "\nGarmin Coach (from today):")
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, w := range sug {
			name := w.Name
			if name == "" && w.DurationS == 0 {
				name = "rest day"
			}
			sport := "-"
			if w.Sport != nil {
				sport = typedef.Sport(*w.Sport).String()
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", w.Day, name, sport, time.Duration(w.DurationS)*time.Second, w.Description)
		}
		tw.Flush()
	}
	sl, err := in.Store.SleepSessions(ctx, u.ID, from, to)
	if err == nil && len(sl) > 0 {
		fmt.Fprintln(stdout, "\nsleep (local time):")
		tw = tabwriter.NewWriter(stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(tw, "day\tfrom\tto\tscore\tdeep\tlight\tREM\tawake\tmin HR\tBB start\tend\t")
		z, _ := in.Store.Zone(ctx, u.ID)
		clock := func(ts int64) string { return time.Unix(ts+z.Offset(ts), 0).UTC().Format("15:04") }
		for _, s := range sl {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", s.Day, clock(s.Start), clock(s.End), i(s.Score),
				minutes(s.Deep), minutes(s.Light), minutes(s.REM), minutes(s.Awake), i(s.MinHR), i(s.BBStart), i(s.BBEnd))
		}
		tw.Flush()
	}
	return 0
}

func i(p *int64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
}

func scaled(p *int64, scale float64, prec int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.*f", prec, float64(*p)*scale)
}

func hours(p *int64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%dh%02d", *p/3600, *p%3600/60)
}

func minutes(p *int64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%dm", *p/60)
}
