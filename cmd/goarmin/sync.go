// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/fitid"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
)

func runSync(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter")
	device := fs.String("device", config.String("GOARMIN_DEVICE", ""), "paired watch address (AA:BB:CC:DD:EE:FF); empty = the only known watch")
	dataDir := fs.String("data", config.DataDir(), "data directory (spool and node state)")
	once := fs.Bool("once", false, "sync once and exit (the only mode until M7)")
	noArchive := fs.Bool("no-archive", false, "never flag files as synced on the watch, so it keeps them")
	includeSynced := fs.Bool("include-synced", false, "also pull files another host already flagged as synced")
	full := fs.Bool("full-listing", false, "ignore the stored listing cursor")
	plain := fs.Bool("plain-gfdi", false, "register GFDI without MLR (max packet 400 instead of 4000)")
	delActivities := fs.Bool("delete-activities", false, "after archiving an activity, delete it from the watch too (no effect with -no-archive)")
	del := fs.String("delete", "", "comma-separated item UIDs (as printed) to delete from the watch after the sync, even with -no-archive; each must be in the spool")
	timeout := fs.Duration("timeout", 30*time.Minute, "give up after this long")
	tracePath := fs.String("trace", "", "write every packet as JSONL here (default ~/goarmin-captures/linux-sync-<time>.jsonl; '-' disables)")
	verbose := fs.Bool("v", false, "log protocol details")
	features := fs.Bool("features", false, "announce the host's feature capabilities as Garmin Connect does (wellness threshold notifications) and print the watch's answer")
	server := fs.String("server", config.String("GOARMIN_SERVER", ""), "run as a sync node for this server: sync on a schedule and hand every file to it (sign in first: goarmin node login)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin sync -server URL [-no-archive]\n       goarmin sync -once [-no-archive] [flags]\n\nWith -server this computer is a sync node: it syncs the watches paired with it every 30\nminutes and when the server asks, hands each file to the server, and flags a file on the\nwatch only once the server has it. Sign in first with goarmin node login.\n\nWith -once it connects to a paired watch, pulls the files it has not handed out yet into the\nlocal spool and, unless -no-archive is given, flags them as synced. The watch deletes health\nfiles once they are flagged; goarmin flags a file only after it is stored.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *server != "" && !*once {
		return runNodeDaemon(*dataDir, *server, *adapter, *noArchive, *features, *verbose, stdout, stderr)
	}
	if !*once {
		fs.Usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	trace, closeTrace, err := openTraceWriter(*tracePath, "linux-sync")
	if err != nil {
		fmt.Fprintln(stderr, "goarmin sync:", err)
		return 1
	}
	defer closeTrace()

	start := time.Now()
	onEvent := func(ev fileaccess.Event) {
		if ev.Kind == fileaccess.EventListed {
			return
		}
		line := fmt.Sprintf("%7.1fs  %-8s %-22s %s", time.Since(start).Seconds(), ev.Kind, ev.Item.TypeName, ev.Item.UID)
		if ev.Bytes > 0 {
			line += "  " + byteSize(int64(ev.Bytes))
		}
		if ev.Err != nil {
			line += "  " + ev.Err.Error()
		}
		fmt.Fprintln(stdout, line)
	}
	mode := "pull and archive"
	if *noArchive {
		mode = "pull only, nothing is flagged on the watch"
	}
	unlock, ok := lockBluetooth("sync", *dataDir, stderr)
	if !ok {
		return 1
	}
	defer unlock()
	fmt.Fprintf(stdout, "goarmin sync (%s)\n", mode)
	res, err := syncnode.SyncOnce(ctx, syncnode.Options{
		DataDir: *dataDir, Adapter: *adapter, Device: *device,
		NoArchive: *noArchive, IncludeSynced: *includeSynced, FullListing: *full, PlainGFDI: *plain,
		DeleteActivities: *delActivities, Delete: splitList(*del),
		Trace: trace, Logger: logger, OnEvent: onEvent, Features: *features,
	})
	printSyncResult(stdout, res)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin sync:", err)
		return 1
	}
	return 0
}

func printSyncResult(out io.Writer, res syncnode.Result) {
	fmt.Fprintln(out, "\n== sync result")
	for _, a := range res.Attempts {
		status := "ok"
		if a.Err != nil {
			status = a.Err.Error()
		}
		fmt.Fprintf(out, "attempt: pair %d, client %d: %s\n", a.Pair, a.Client, status)
	}
	if res.Device.ProductNumber == 0 {
		return
	}
	d := res.Device
	gfdiMode := "plain"
	if res.Reliable {
		gfdiMode = "reliable (MLR)"
	}
	fmt.Fprintf(out, "watch: %s, product %d, firmware %s, unit %d\n", d.BluetoothName, d.ProductNumber, d.SoftwareVersionString(), d.UnitNumber)
	fmt.Fprintf(out, "link: pair %d, client %d, GFDI %s, max packet %d, connected and handshake done after %s\n",
		res.Pair, res.Client, gfdiMode, d.MaxPacketSize, res.Handshake.Round(100*time.Millisecond))
	r := res.Report
	fmt.Fprintf(out, "this run: listed %d (ignored %d), new %d, pulled %d (%s) in %s; archived %d, deferred %d, kept %d, gone %d, failed %d\n",
		r.Listed, r.Ignored, r.Added, r.Pulled, byteSize(r.Bytes), res.SyncTime.Round(100*time.Millisecond),
		r.Archived, r.Deferred, r.Kept, r.Gone, r.Failed)
	if r.WellnessCut != "" {
		fmt.Fprintf(out, "wellness files cut: %s\n", r.WellnessCut)
	}
	if f := res.Features; f != nil {
		fmt.Fprintf(out, "watch features: GUID %s, version %d, wellness support %v, lifestyle logging %v, custom flags %v, fields %v\n",
			f.GUIDStatus, f.Version, f.WellnessSupport, f.LifestyleLoggingSupport, f.CustomFlagSupport, f.Fields)
	}
	if res.WellnessMet > 0 {
		fmt.Fprintf(out, "wellness threshold met notifications: %d\n", res.WellnessMet)
	}
	if r.Deleted+r.Refused > 0 {
		fmt.Fprintf(out, "deleted from the watch after archiving: %d, refused by the watch: %d\n", r.Deleted, r.Refused)
	}
	for _, d := range res.Deleted {
		status := "deleted from the watch (the spool keeps the file)"
		if d.Err != nil {
			status = "NOT deleted: " + d.Err.Error()
		}
		fmt.Fprintf(out, "delete %s: %s\n", d.UID, status)
	}
	if r.Pulled > 0 && res.SyncTime > 0 {
		fmt.Fprintf(out, "throughput: %s/s (inflated)\n", byteSize(int64(float64(r.Bytes)/res.SyncTime.Seconds())))
	}

	// Everything in the spool, by type, with a FIT check of each file.
	type agg struct {
		n, fitOK, serialOK int
		bytes              int64
		errs               []string
	}
	byType := map[string]*agg{}
	for _, f := range res.Files {
		a := byType[f.TypeName]
		if a == nil {
			a = &agg{}
			byType[f.TypeName] = a
		}
		a.n++
		data, err := os.ReadFile(f.Path)
		if err != nil {
			a.errs = append(a.errs, err.Error())
			continue
		}
		a.bytes += int64(len(data))
		if !fitid.Looks(data) {
			continue
		}
		info, err := fitid.Inspect(data)
		if err != nil {
			a.errs = append(a.errs, filepath.Base(f.Path)+": "+err.Error())
			continue
		}
		a.fitOK++
		if info.SerialNumber == d.UnitNumber {
			a.serialOK++
		}
	}
	if len(byType) == 0 {
		fmt.Fprintf(out, "spool %s: empty\n", res.StoreDir)
		return
	}
	fmt.Fprintf(out, "spool %s:\n", res.StoreDir)
	names := make([]string, 0, len(byType))
	for n := range byType {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := byType[n]
		fmt.Fprintf(out, "  %-22s %3d files %9s  FIT valid %d/%d, file_id serial = unit %d/%d\n", n, a.n, byteSize(a.bytes), a.fitOK, a.n, a.serialOK, a.fitOK)
		for _, e := range a.errs {
			fmt.Fprintln(out, "    !", e)
		}
	}
}

// splitList splits a comma-separated flag value, dropping empty entries.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// openTraceWriter opens the JSONL trace: path, or ~/goarmin-captures/<prefix>-<time>.jsonl when
// empty, or nothing for "-".
func openTraceWriter(path, prefix string) (io.Writer, func(), error) {
	if path == "-" {
		return nil, func() {}, nil
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		dir := filepath.Join(home, "goarmin-captures")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, err
		}
		path = filepath.Join(dir, prefix+"-"+time.Now().Format("20060102-150405")+".jsonl")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(os.Stderr, "trace: %s\n", strings.TrimSpace(path))
	return f, func() { f.Close() }, nil
}
