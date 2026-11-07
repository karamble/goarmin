// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command wellnessdump prints what pkg/garmin/wellness extracts from FIT files: sample counts
// and ranges per kind, time zone offsets, daily values and sleep nights.
//
//	go run ./tools/wellnessdump [-samples] <file or directory>...
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func main() {
	samples := flag.Bool("samples", false, "print every sample")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: wellnessdump [-samples] <file or directory>...")
		os.Exit(2)
	}
	for _, root := range flag.Args() {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".fit") {
				return err
			}
			dump(p, *samples)
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}

func dump(path string, all bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(path, err)
		return
	}
	d, err := wellness.Decode(b)
	if err != nil {
		fmt.Printf("== %s: %v\n", path, err)
		return
	}
	from, to := d.Span()
	if from.IsZero() {
		return // nothing we know in this file
	}
	fmt.Printf("== %s  %s .. %s\n", path, from.Format(time.DateTime), to.Format(time.DateTime))
	type agg struct {
		n        int
		min, max int64
	}
	per := map[wellness.Kind]*agg{}
	for _, s := range d.Samples {
		a := per[s.Kind]
		if a == nil {
			a = &agg{min: s.Value, max: s.Value}
			per[s.Kind] = a
		}
		a.n++
		a.min, a.max = min(a.min, s.Value), max(a.max, s.Value)
		if all {
			fmt.Printf("   %s %-16s sub=%-3d %d\n", s.At.Format(time.DateTime), s.Kind, s.Sub, s.Value)
		}
	}
	ks := make([]wellness.Kind, 0, len(per))
	for k := range per {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	for _, k := range ks {
		a := per[k]
		sc := k.Info().Scale
		fmt.Printf("   %-18s n=%-5d %g .. %g %s\n", k, a.n, float64(a.min)*sc, float64(a.max)*sc, k.Info().Unit)
	}
	for _, o := range d.Offsets {
		fmt.Printf("   offset from %s: %s\n", o.At.Format(time.DateTime), o.Offset)
	}
	for _, v := range d.Daily {
		fmt.Printf("   daily %-18s %s day=%q %g\n", v.Kind, v.At.Format(time.DateTime), v.Day, float64(v.Value)*v.Kind.Info().Scale)
	}
	for _, s := range d.Sleep {
		fmt.Printf("   sleep %s .. %s score=%d quality=%d min_hr=%d bb=%d→%d deep=%d light=%d rem=%d awake=%d (min)\n",
			s.Start.Format(time.DateTime), s.End.Format(time.DateTime), s.Score, s.QualityScore, s.MinHR,
			s.BodyBatteryStart, s.BodyBatteryEnd, mins(s.Deep), mins(s.Light), mins(s.REM), mins(s.Awake))
	}
	for _, h := range d.HRV {
		fmt.Printf("   hrv %s last night %.0f ms, weekly %.0f ms, status %d\n", h.At.Format(time.DateTime), h.LastNight, h.WeeklyAverage, h.Status)
	}
}

func mins(s int) int {
	if s < 0 {
		return s
	}
	return s / 60
}
