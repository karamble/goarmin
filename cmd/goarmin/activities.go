// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitactivity"
)

func runActivities(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("activities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	user := fs.String("user", config.String("GOARMIN_USER", ""), "email of the account")
	n := fs.Int("n", 20, "how many, newest first")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *user == "" {
		fmt.Fprintln(stderr, "usage: goarmin activities -user EMAIL [-n N]")
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	in, closeData, err := openData(ctx, *dataDir, nil)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin activities:", err)
		return 1
	}
	defer closeData()
	u, err := in.Store.UserByEmail(ctx, *user)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin activities:", err)
		return 1
	}
	acts, err := in.Store.Activities(ctx, u.ID, 0, *n)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin activities:", err)
		return 1
	}
	z, _ := in.Store.Zone(ctx, u.ID)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tstart\tname\tsport\ttime\tkm\tpace/speed\tHR avg/max\tascent\tTE\tGPS")
	for _, a := range acts {
		start := time.Unix(a.Start+z.Offset(a.Start), 0).UTC().Format("2006-01-02 15:04")
		dur := "-"
		if a.Timer != nil {
			dur = (time.Duration(*a.Timer) * time.Second).String()
		}
		km := "-"
		if a.Distance != nil && *a.Distance > 0 {
			km = fmt.Sprintf("%.2f", *a.Distance/1000)
		}
		gps := ""
		if a.Polyline != "" {
			gps = "track"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s/%s\t%s\t%s\t%s\n", a.ID, start, a.Name,
			fitactivity.Label(typedef.Sport(a.Sport), typedef.SubSport(a.SubSport), a.Name), dur, km, pace(a), i(a.AvgHR), i(a.MaxHR),
			i(a.Ascent), f1(a.TrainingEffect), gps)
	}
	tw.Flush()
	return 0
}

// pace is min/km for foot sports, km/h otherwise.
func pace(a store.ActivitySummary) string {
	if a.AvgSpeed == nil || *a.AvgSpeed <= 0 {
		return "-"
	}
	switch typedef.Sport(a.Sport) {
	case typedef.SportSwimming:
		s := 100 / *a.AvgSpeed
		return fmt.Sprintf("%d:%02d/100m", int(s)/60, int(s)%60)
	case typedef.SportRunning, typedef.SportWalking, typedef.SportHiking:
		s := 1000 / *a.AvgSpeed
		return fmt.Sprintf("%d:%02d/km", int(s)/60, int(s)%60)
	}
	return fmt.Sprintf("%.1f km/h", *a.AvgSpeed*3.6)
}

func f1(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *p)
}
