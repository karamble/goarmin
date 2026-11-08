// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command fitinspect walks files and directories, decodes every FIT file it
// finds (with github.com/muktihari/fit), and reports which FIT messages and
// fields are present, per device and per folder. Messages and fields that the
// official FIT profile does not define are listed with value ranges, as hints
// to what they mean (e.g. Body Battery in field 3 of stress_level).
//
// Usage:
//
//	go run ./tools/fitinspect <dir-or-file>...
//
// FIT files from a watch contain personal health data: keep them outside the
// repository.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitprofile"
	"github.com/muktihari/fit/proto"
)

type fieldStat struct {
	name     string
	unknown  bool
	count    int
	numeric  bool
	min, max float64
	sample   string
}

type mesgStat struct {
	num       typedef.MesgNum
	count     int
	devFields int
	files     map[string]bool
	folders   map[string]bool
	fields    map[byte]*fieldStat
}

type fileResult struct {
	path     string
	folder   string
	fileType string
	device   string
	created  string
	profile  uint16
	mesgs    int
	chained  int
	err      error
}

type report struct {
	files   []fileResult
	mesgs   map[typedef.MesgNum]*mesgStat
	devices map[string]int
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fitinspect <dir-or-file>...")
		os.Exit(2)
	}
	r := &report{mesgs: map[typedef.MesgNum]*mesgStat{}, devices: map[string]int{}}
	for _, root := range os.Args[1:] {
		if err := r.walk(root); err != nil {
			fmt.Fprintf(os.Stderr, "walk %s: %v\n", root, err)
			os.Exit(1)
		}
	}
	r.print()
}

func (r *report) walk(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".fit") {
			return nil
		}
		folder := "."
		if rel, err := filepath.Rel(root, filepath.Dir(path)); err == nil {
			folder = rel
		}
		r.files = append(r.files, r.inspect(path, folder))
		return nil
	})
}

func (r *report) inspect(path, folder string) fileResult {
	res := fileResult{path: path, folder: folder}
	f, err := os.Open(path)
	if err != nil {
		res.err = err
		return res
	}
	defer f.Close()

	dec := decoder.New(f, decoder.WithNoComponentExpansion())
	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			res.err = err
			break
		}
		res.chained++
		res.profile = fit.FileHeader.ProfileVersion
		for i := range fit.Messages {
			m := &fit.Messages[i]
			res.mesgs++
			if m.Num == typedef.MesgNumFileId && res.fileType == "" {
				id := mesgdef.NewFileId(m)
				_, product := id.GetProduct()
				res.fileType = id.Type.String()
				res.device = fmt.Sprintf("%v serial=%d", product, id.SerialNumber)
				if !id.TimeCreated.IsZero() {
					res.created = id.TimeCreated.UTC().Format("2006-01-02T15:04:05Z")
				}
				r.devices[res.device]++
			}
			r.add(m, path, folder)
		}
	}
	return res
}

func (r *report) add(m *proto.Message, path, folder string) {
	s := r.mesgs[m.Num]
	if s == nil {
		s = &mesgStat{num: m.Num, files: map[string]bool{}, folders: map[string]bool{}, fields: map[byte]*fieldStat{}}
		r.mesgs[m.Num] = s
	}
	s.count++
	s.devFields += len(m.DeveloperFields)
	s.files[path] = true
	s.folders[folder] = true
	for i := range m.Fields {
		fld := &m.Fields[i]
		fs := s.fields[fld.Num]
		if fs == nil {
			fs = &fieldStat{name: fld.Name, unknown: fld.Name == "unknown"}
			s.fields[fld.Num] = fs
		}
		fs.count++
		v := fld.Value.Any()
		if fs.sample == "" {
			fs.sample = truncate(fmt.Sprint(v), 48)
		}
		if x, ok := toFloat(v); ok {
			if !fs.numeric {
				fs.numeric, fs.min, fs.max = true, x, x
			} else {
				fs.min, fs.max = min(fs.min, x), max(fs.max, x)
			}
		}
	}
}

func (r *report) print() {
	var errs []fileResult
	for _, f := range r.files {
		if f.err != nil {
			errs = append(errs, f)
		}
	}
	fmt.Printf("== FIT files: %d (decode errors: %d)\n\n", len(r.files), len(errs))

	fmt.Println("== Devices (from file_id)")
	for _, d := range sortedKeys(r.devices) {
		fmt.Printf("  %-40s %d files\n", d, r.devices[d])
	}

	fmt.Println("\n== Folders")
	type folderAgg struct {
		files int
		types map[string]int
	}
	folders := map[string]*folderAgg{}
	for _, f := range r.files {
		a := folders[f.folder]
		if a == nil {
			a = &folderAgg{types: map[string]int{}}
			folders[f.folder] = a
		}
		a.files++
		t := f.fileType
		if t == "" {
			t = "(no file_id)"
		}
		a.types[t]++
	}
	for _, name := range sortedKeys(folders) {
		a := folders[name]
		var parts []string
		for _, t := range sortedKeys(a.types) {
			parts = append(parts, fmt.Sprintf("%s×%d", t, a.types[t]))
		}
		fmt.Printf("  %-28s %5d files  %s\n", name, a.files, strings.Join(parts, ", "))
	}

	fmt.Println("\n== Messages (number, name, count, files, folders; * = named by the community profile)")
	nums := make([]typedef.MesgNum, 0, len(r.mesgs))
	for n := range r.mesgs {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	for _, n := range nums {
		s := r.mesgs[n]
		// Garmin's own name, else the community profile's (marked *), else none.
		name := n.String()
		pm, inProfile := fitprofile.Lookup(uint16(n))
		if strings.HasPrefix(name, "MesgNumInvalid(") {
			name = "UNDOCUMENTED"
			if inProfile {
				name = pm.Name + " *"
			}
		}
		fmt.Printf("\n  %5d %-28s count=%-8d files=%-5d folders=%s\n",
			uint16(n), name, s.count, len(s.files), strings.Join(sortedKeys(s.folders), ","))
		if s.devFields > 0 {
			fmt.Printf("        developer fields: %d\n", s.devFields)
		}
		fieldNums := make([]int, 0, len(s.fields))
		for fn := range s.fields {
			fieldNums = append(fieldNums, int(fn))
		}
		sort.Ints(fieldNums)
		var known []string
		for _, fn := range fieldNums {
			fs := s.fields[byte(fn)]
			if !fs.unknown {
				known = append(known, fmt.Sprintf("%d:%s", fn, fs.name))
			}
		}
		if len(known) > 0 {
			fmt.Printf("        known fields: %s\n", strings.Join(known, " "))
		}
		for _, fn := range fieldNums {
			fs := s.fields[byte(fn)]
			if !fs.unknown {
				continue
			}
			rng := ""
			if fs.numeric {
				rng = fmt.Sprintf(" min=%g max=%g", fs.min, fs.max)
			}
			if f, ok := pm.Field(byte(fn)); ok && inProfile {
				// Raw values; the community profile's scale and units say what they mean.
				meaning := f.Type
				if f.Units != "" {
					meaning += " " + f.Units
				}
				if f.Scale != 0 && f.Scale != 1 {
					meaning += fmt.Sprintf(" /%g", f.Scale)
				}
				if f.Offset != 0 {
					meaning += fmt.Sprintf(" -%g", f.Offset)
				}
				fmt.Printf("        community field %3d %s (%s): count=%d%s sample=%s\n", fn, f.Name, meaning, fs.count, rng, fs.sample)
				continue
			}
			fmt.Printf("        undocumented field %3d: count=%d%s sample=%s\n", fn, fs.count, rng, fs.sample)
		}
	}

	if len(errs) > 0 {
		fmt.Println("\n== Decode errors")
		for _, f := range errs {
			fmt.Printf("  %s: %v\n", f.path, f.err)
		}
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int8:
		return float64(x), true
	case uint8:
		return float64(x), true
	case int16:
		return float64(x), true
	case uint16:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
