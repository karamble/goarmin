// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command fitprofilegen writes pkg/fitprofile: every FIT message, field, enumeration, file type
// and device goarmin can name. The official messages and fields come from the Garmin FIT profile
// as github.com/muktihari/fit carries it (names, types, scale, offset, units); the messages and
// fields Garmin does not document, the enumerations, the file types and the devices come from
// Gadgetbridge's community-maintained description, read from a local clone:
//
//	go run ./tools/fitprofilegen -gadgetbridge ~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge
//
// It writes pkg/fitprofile/profile_gen.go and web/src/lib/fitTypes.gen.ts (file type names).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/typedef"
)

func main() {
	gb := flag.String("gadgetbridge", os.Getenv("GADGETBRIDGE"), "Gadgetbridge clone (its FitCodeGenerator and garmin/FileType.java are read)")
	goOut := flag.String("go", "pkg/fitprofile/profile_gen.go", "Go file to write")
	tsOut := flag.String("ts", "web/src/lib/fitTypes.gen.ts", "TypeScript file to write (file type names)")
	flag.Parse()
	if *gb == "" {
		fmt.Fprintln(os.Stderr, "fitprofilegen: -gadgetbridge (or GADGETBRIDGE) names the Gadgetbridge clone")
		os.Exit(2)
	}
	p, err := Build(*gb)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fitprofilegen:", err)
		os.Exit(1)
	}
	goSrc, err := p.Go()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fitprofilegen:", err)
		os.Exit(1)
	}
	for path, b := range map[string][]byte{*goOut: goSrc, *tsOut: p.TS()} {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fitprofilegen:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("%d messages (%d official), %d fields, %d enumerations, %d file types, %d other files, %d devices\n",
		len(p.Messages), p.official(), p.fields(), len(p.Enums), len(p.FileTypes), len(p.OtherFiles), len(p.Devices))
}

// Profile is what the generated package holds.
type Profile struct {
	Messages   []Message
	Enums      []Enum
	FileTypes  []FileType
	OtherFiles []OtherFile
	Devices    []Device
}

type Message struct {
	Num      uint16
	Name     string
	Official bool
	Fields   []Field
}

type Field struct {
	Num      uint8
	Name     string
	Base     string
	Type     string
	Units    string
	Scale    float64
	Offset   float64
	Array    bool
	ArrayLen int
	Official bool
}

type Enum struct {
	Name    string
	Entries []Entry
}

type Entry struct {
	Num  uint32
	Name string
}

type FileType struct {
	Num      uint8
	Name     string
	Title    string
	Official bool
	Archive  bool
}

type OtherFile struct {
	Type, Subtype uint8
	Name          string
}

type Device struct {
	Manufacturer, Product uint16
	Name                  string
}

func (p *Profile) official() (n int) {
	for _, m := range p.Messages {
		if m.Official {
			n++
		}
	}
	return n
}

func (p *Profile) fields() (n int) {
	for _, m := range p.Messages {
		n += len(m.Fields)
	}
	return n
}

// The Gadgetbridge profile's JSON.
type gbProfile struct {
	Messages []struct {
		Num    int    `json:"num"`
		Name   string `json:"name"`
		Fields []struct {
			Num      int      `json:"num"`
			Name     string   `json:"name"`
			Base     string   `json:"base"`
			Type     string   `json:"type"`
			UOM      string   `json:"UOM"`
			Scale    *float64 `json:"scale"`
			Offset   *float64 `json:"offset"`
			ArrayLen int      `json:"arrayLen"`
		} `json:"fields"`
	} `json:"messages"`
	Enumerations []struct {
		Name    string `json:"name"`
		Entries []struct {
			Num  int64  `json:"num"`
			Name string `json:"name"`
		} `json:"entries"`
	} `json:"enumerations"`
	Devices []struct {
		Manufacturer int    `json:"manufacturer"`
		Product      int    `json:"product"`
		Name         string `json:"name"`
	} `json:"devices"`
}

// Build reads the Gadgetbridge clone at gb and merges it with the official profile.
func Build(gb string) (*Profile, error) {
	raw, err := os.ReadFile(filepath.Join(gb, "FitCodeGenerator", "src", "main", "resources", "fit_profile.json"))
	if err != nil {
		return nil, err
	}
	var g gbProfile
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("fit_profile.json: %w", err)
	}
	java, err := os.ReadFile(filepath.Join(gb, "app", "src", "main", "java", "nodomain", "freeyourgadget", "gadgetbridge",
		"service", "devices", "garmin", "FileType.java"))
	if err != nil {
		return nil, err
	}
	p := &Profile{}

	// Messages: every official one with its official fields, then the community's.
	byNum := map[uint16]*Message{}
	for n := 0; n <= 0xFFFF; n++ {
		name := typedef.MesgNum(n).String()
		if strings.HasPrefix(name, "MesgNumInvalid") {
			continue
		}
		m := &Message{Num: uint16(n), Name: name, Official: true}
		for _, f := range factory.CreateMesg(typedef.MesgNum(n)).Fields {
			m.Fields = append(m.Fields, Field{Num: f.Num, Name: f.Name, Base: f.BaseType.String(), Type: snake(f.Type.String()),
				Units: f.Units, Scale: f.Scale, Offset: f.Offset, Array: f.Array, Official: true})
		}
		byNum[m.Num] = m
	}
	for _, gm := range g.Messages {
		if gm.Num < 0 || gm.Num > 0xFFFF {
			continue
		}
		m := byNum[uint16(gm.Num)]
		if m == nil {
			m = &Message{Num: uint16(gm.Num), Name: snake(gm.Name)}
			byNum[m.Num] = m
		}
		have := map[uint8]bool{}
		for _, f := range m.Fields {
			have[f.Num] = true
		}
		for _, gf := range gm.Fields {
			if gf.Num < 0 || gf.Num > 255 || have[uint8(gf.Num)] {
				continue
			}
			f := Field{Num: uint8(gf.Num), Name: snake(gf.Name), Base: strings.ToLower(gf.Base), Type: snake(gf.Type),
				Units: unit(gf.UOM), Scale: 1, ArrayLen: gf.ArrayLen, Array: gf.ArrayLen > 0}
			if f.Type == "" {
				f.Type = f.Base
			}
			if gf.Scale != nil && *gf.Scale != 0 {
				f.Scale = *gf.Scale
			}
			if gf.Offset != nil {
				f.Offset = *gf.Offset
			}
			m.Fields = append(m.Fields, f)
			have[f.Num] = true
		}
	}
	for _, m := range byNum {
		sort.Slice(m.Fields, func(a, b int) bool { return m.Fields[a].Num < m.Fields[b].Num })
		p.Messages = append(p.Messages, *m)
	}
	sort.Slice(p.Messages, func(a, b int) bool { return p.Messages[a].Num < p.Messages[b].Num })

	for _, ge := range g.Enumerations {
		e := Enum{Name: enumName(ge.Name)}
		for _, x := range ge.Entries {
			if x.Num < 0 || x.Num > 0xFFFFFFFF {
				continue
			}
			e.Entries = append(e.Entries, Entry{Num: uint32(x.Num), Name: snake(x.Name)})
		}
		sort.Slice(e.Entries, func(a, b int) bool { return e.Entries[a].Num < e.Entries[b].Num })
		p.Enums = append(p.Enums, e)
	}
	sort.Slice(p.Enums, func(a, b int) bool { return p.Enums[a].Name < p.Enums[b].Name })

	if err := p.fileTypes(string(java)); err != nil {
		return nil, err
	}

	seen := map[[2]uint16]bool{}
	for _, d := range g.Devices {
		if d.Manufacturer < 0 || d.Manufacturer > 0xFFFF || d.Product < 0 || d.Product > 0xFFFF {
			continue
		}
		k := [2]uint16{uint16(d.Manufacturer), uint16(d.Product)}
		if seen[k] || strings.TrimSpace(d.Name) == "" {
			continue
		}
		seen[k] = true
		p.Devices = append(p.Devices, Device{Manufacturer: k[0], Product: k[1], Name: strings.TrimSpace(d.Name)})
	}
	sort.Slice(p.Devices, func(a, b int) bool {
		if p.Devices[a].Manufacturer != p.Devices[b].Manufacturer {
			return p.Devices[a].Manufacturer < p.Devices[b].Manufacturer
		}
		return p.Devices[a].Product < p.Devices[b].Product
	})
	return p, nil
}

// fileJava matches Gadgetbridge's FILETYPE entries: NAME(type, subtype[, archive][, "garmin name"]).
var fileJava = regexp.MustCompile(`(?m)^\s+([A-Z0-9_]+)\(\s*(\d+)\s*,\s*(\d+)\s*(?:,\s*(true|false))?\s*(?:,\s*"[^"]*")?\s*\)\s*[,;]`)

// titles are the file types' names in words, where the profile's name is no word.
var titles = map[uint8]string{
	1: "Device", 2: "Settings", 3: "Sport profile", 4: "Activity", 5: "Workout", 6: "Course", 7: "Schedule",
	8: "Locations", 9: "Weight", 10: "Totals", 11: "Goals", 12: "Map", 13: "Debug", 14: "Blood pressure", 15: "Monitoring",
	20: "Activity summary", 21: "Glucose", 22: "Tracking records", 23: "Tracking events", 25: "Vector",
	28: "Daily totals", 29: "Records", 30: "Alert", 32: "Monitoring", 33: "Multisport", 34: "Segment", 35: "Segment list",
	36: "Golf", 37: "Golf clubs", 38: "Score", 39: "Adjustments", 40: "HMD", 41: "Settings change log", 44: "Training metrics",
	45: "Bat swing", 46: "Roster", 47: "Dive plan", 48: "Health snapshot data", 49: "Sleep", 50: "Software",
	51: "Challenge result", 52: "User behaviour log", 53: "Chronograph round", 54: "Chronograph shot",
	55: "Chronograph scorecard", 56: "Pace bands", 57: "Sports backup", 58: "Device data", 59: "Muscle map",
	60: "Running track", 61: "ECG", 62: "Benchmark", 63: "Power guidance", 65: "Calendar", 68: "HRV status",
	70: "Health snapshot", 71: "Community activity", 72: "Daily health", 73: "Skin temperature", 74: "Physiology backup",
	77: "Garmin Coach", 79: "Sleep disruption", 82: "Area courses", 87: "Gear",
}

func (p *Profile) fileTypes(java string) error {
	types := map[uint8]*FileType{}
	for n := 0; n <= 255; n++ {
		name := typedef.File(n).String()
		if strings.HasPrefix(name, "FileInvalid") {
			continue
		}
		types[uint8(n)] = &FileType{Num: uint8(n), Name: snake(name), Official: true}
	}
	matches := fileJava.FindAllStringSubmatch(java, -1)
	if len(matches) < 50 {
		return fmt.Errorf("FileType.java: found only %d file types", len(matches))
	}
	for _, m := range matches {
		typ, _ := strconv.Atoi(m[2])
		sub, _ := strconv.Atoi(m[3])
		if typ > 255 || sub > 255 {
			continue
		}
		name := snake(m[1])
		if typ != 128 {
			p.OtherFiles = append(p.OtherFiles, OtherFile{Type: uint8(typ), Subtype: uint8(sub), Name: name})
			continue
		}
		t := types[uint8(sub)]
		if t == nil {
			t = &FileType{Num: uint8(sub), Name: name}
			types[t.Num] = t
		} else if strings.HasPrefix(t.Name, "file_type_") || t.Name == "" {
			t.Name = name
		}
		t.Archive = m[4] == "true"
	}
	for _, t := range types {
		t.Title = titles[t.Num]
		if t.Title == "" {
			t.Title = words(t.Name)
		}
		p.FileTypes = append(p.FileTypes, *t)
	}
	sort.Slice(p.FileTypes, func(a, b int) bool { return p.FileTypes[a].Num < p.FileTypes[b].Num })
	sort.Slice(p.OtherFiles, func(a, b int) bool {
		if p.OtherFiles[a].Type != p.OtherFiles[b].Type {
			return p.OtherFiles[a].Type < p.OtherFiles[b].Type
		}
		return p.OtherFiles[a].Subtype < p.OtherFiles[b].Subtype
	})
	return nil
}

// units turns the community profile's unit names into the symbols the official profile uses.
var units = map[string]string{
	"AMPERE": "A", "BAR": "bar", "BAR_PER_MINUTE": "bar/min", "BEATS_PER_MINUTE": "bpm", "BREATHS_PER_MINUTE": "breaths/min",
	"BREATH_PER_MINUTE": "breaths/min", "BYTES": "bytes", "CELSIUS": "C", "CENTIMETER": "cm", "CENTIMETER_PER_SECOND": "cm/s",
	"COUNT": "counts", "CYCLES": "cycles", "DAYS": "days", "DEGREES": "degrees", "DEGREES_PER_SECOND": "deg/s",
	"FLOORS": "floors", "FLOW": "Flow", "GAUSS": "G", "GRAIN": "grain", "GRAMM": "g", "GRAM_PER_DECILITER": "g/dL",
	"HOURS": "h", "JOULE": "J", "KCAL": "kcal", "KCAL_PER_CYCLE": "kcal/cycle", "KCAL_PER_DAY": "kcal/day",
	"KCAL_PER_MINUTE": "kcal/min", "KILOGRAM": "kg", "KILOGRAM_PER_QUBIC_METER": "kg/m^3", "KILOGRAM_PER_SQUARE_METER": "kg/m^2",
	"KILOMETER": "km", "KILOMETER_PER_HOUR": "km/h", "KILO_GRIT": "kGrit", "LENGTHS": "lengths", "LITER": "L",
	"LITER_PER_MINUTE": "L/min", "METER": "m", "METER_PER_CYCLE": "m/cycle", "METER_PER_HOUR": "m/h", "METER_PER_SECOND": "m/s",
	"METER_PER_SQUARE_SECOND": "m/s^2", "MILES_PER_SECOND": "mi/s", "MILLIGRAVITY": "mG", "MILLILITER": "mL",
	"MILLIMETER": "mm", "MILLIMETER_OF_MERCURY": "mmHg", "MILLISECONDS": "ms", "MINUTES": "min", "ML_KG_MIN": "mL/kg/min",
	"NEWTON": "N", "OXYGEN_TOXICITY_UNITs": "OTUs", "PASCAL": "Pa", "PERCENT": "%", "RADIANS": "radians",
	"RADIANS_PER_SECOND": "rad/s", "REVOLUTIONS_PER_MINUTE": "rpm", "SECONDS": "s", "SEMICIRCLES": "semicircles",
	"STANDARD_GRAVITY": "g", "STEPS": "steps", "STORKES_PER_LAP": "strokes/lap", "STROKES": "strokes",
	"STROKES_PER_LENGTH": "strokes/length", "STROKES_PER_MINUTE": "strokes/min", "VOLT": "V", "WATT": "watts", "YEARS": "years",
}

// unit gives a unit's symbol; a name the table does not know stays as it is.
func unit(s string) string {
	if u, ok := units[s]; ok {
		return u
	}
	return s
}

// enumName fixes a known misspelling in the community profile while making the name snake case.
func enumName(s string) string {
	if s == "ActivitiyType" {
		s = "ActivityType"
	}
	return snake(s)
}

// snake turns "ECG_SUMMARY", "SleepLevel", "HRVStatus" or "file_id" into snake case.
func snake(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch {
		case r == '_' || r == ' ' || r == '-':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		case unicode.IsUpper(r):
			prevLower := i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]))
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1]) && i > 0 && unicode.IsUpper(rs[i-1])
			if (prevLower || nextLower) && b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// words turns a snake name into a title: "running_track" → "Running track".
func words(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

const header = `// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

`

// Go renders the generated Go file.
func (p *Profile) Go() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("// Code generated by tools/fitprofilegen; DO NOT EDIT.\n\npackage fitprofile\n\n")
	b.WriteString("var messages = []Message{\n")
	for _, m := range p.Messages {
		fmt.Fprintf(&b, "\t{Num: %d, Name: %q, Official: %t, Fields: []Field{\n", m.Num, m.Name, m.Official)
		for _, f := range m.Fields {
			fmt.Fprintf(&b, "\t\t{Num: %d, Name: %q, Base: %q, Type: %q", f.Num, f.Name, f.Base, f.Type)
			if f.Units != "" {
				fmt.Fprintf(&b, ", Units: %q", f.Units)
			}
			if f.Scale != 1 && f.Scale != 0 {
				fmt.Fprintf(&b, ", Scale: %s", num(f.Scale))
			}
			if f.Offset != 0 {
				fmt.Fprintf(&b, ", Offset: %s", num(f.Offset))
			}
			if f.Array {
				fmt.Fprintf(&b, ", Array: true")
			}
			if f.ArrayLen > 0 {
				fmt.Fprintf(&b, ", ArrayLen: %d", f.ArrayLen)
			}
			fmt.Fprintf(&b, ", Official: %t},\n", f.Official)
		}
		b.WriteString("\t}},\n")
	}
	b.WriteString("}\n\nvar enums = []Enum{\n")
	for _, e := range p.Enums {
		fmt.Fprintf(&b, "\t{Name: %q, Entries: []Entry{", e.Name)
		for i, x := range e.Entries {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "{%d, %q}", x.Num, x.Name)
		}
		b.WriteString("}},\n")
	}
	b.WriteString("}\n\nvar fileTypes = []FileType{\n")
	for _, t := range p.FileTypes {
		fmt.Fprintf(&b, "\t{Num: %d, Name: %q, Title: %q, Official: %t, Archive: %t},\n", t.Num, t.Name, t.Title, t.Official, t.Archive)
	}
	b.WriteString("}\n\nvar otherFiles = []OtherFile{\n")
	for _, o := range p.OtherFiles {
		fmt.Fprintf(&b, "\t{Type: %d, Subtype: %d, Name: %q},\n", o.Type, o.Subtype, o.Name)
	}
	b.WriteString("}\n\nvar devices = []Device{\n")
	for _, d := range p.Devices {
		fmt.Fprintf(&b, "\t{%d, %d, %q},\n", d.Manufacturer, d.Product, d.Name)
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func num(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// TS renders the dashboard's file type names.
func (p *Profile) TS() []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("// Code generated by tools/fitprofilegen; DO NOT EDIT.\n\n")
	b.WriteString("/** The FIT file types by number, in words. */\nexport const fitTypeTitles: Record<number, string> = {\n")
	for _, t := range p.FileTypes {
		fmt.Fprintf(&b, "  %d: %s,\n", t.Num, tsString(t.Title))
	}
	b.WriteString("}\n")
	return b.Bytes()
}

func tsString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}
