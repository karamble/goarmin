// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitnamed"
	"github.com/karamble/goarmin/pkg/fitprofile"
)

// The file types beyond the official ones that this package reads (Garmin's numbers, as the
// community profile names them).
const (
	FileRecords    typedef.File = 29 // personal records
	FileMultisport typedef.File = 33 // multisport settings
	FileClubs      typedef.File = 37 // golf clubs
	FileChangelog  typedef.File = 41 // settings change log
	FileDeviceData typedef.File = 58 // device data: solar charge
	FileGear       typedef.File = 87 // gear
)

// GearItem is gear the watch keeps (shoes, a bike), as the watch names it.
type GearItem struct {
	UUID         string `json:"uuid"`
	Type         int64  `json:"type"`
	Name         string `json:"name"`
	Brand        string `json:"brand,omitempty"`
	Model        string `json:"model,omitempty"`
	MaxUse       int64  `json:"max_use,omitempty"` // the threshold the watch warns at, in its units
	UsageTracked bool   `json:"usage_tracked"`
}

// Club is a golf club in the watch's bag.
type Club struct {
	Index       int     `json:"index"`
	Type        int64   `json:"type"`
	Name        string  `json:"name,omitempty"`
	AvgDistance float64 `json:"avg_distance_m,omitempty"`
	MaxDistance float64 `json:"max_distance_m,omitempty"`
	Loft, Lie   float64 `json:"-"`
}

// Record is a personal record the watch keeps (its metric numbers are Garmin's).
type Record struct {
	At       time.Time
	Metric   int64
	Sport    string
	Distance float64 // m, for records over a distance
	Value    int64   // as the watch stores it (a time in ms, a distance in cm, …)
}

// Change is one entry of the watch's settings change log: which message and field changed to
// what.
type Change struct {
	At      time.Time
	Message string // the message's name (the FIT profile's, or message_<n>)
	Field   string // the field's name (or field_<n>)
	Value   string // the new value, in words where the profile knows them
	Kind    string // what kind of change (settings_change_type)
	Sport   string
}

// Solar is the solar charge at a moment: intensity in percent and the battery time it gained.
type Solar struct {
	At      time.Time
	Percent float64
	GainMS  int64
}

// Doc is a file's messages that no table reads, kept as a readable document: message name →
// the messages as name → value.
type Doc map[string][]map[string]any

// read handles the messages of the device-data files: the tables' own, and with docs every
// other message not in skip, into the file's Doc.
func (f *File) read(m *proto.Message, docs bool, skip map[typedef.MesgNum]bool) {
	if m.Num == 318 {
		// Solar charge comes once a minute: read straight, without naming every field.
		var at, pct, gain int64
		var hasAt, hasPct bool
		for i := range m.Fields {
			fl := &m.Fields[i]
			if fl.IsExpandedField || !fl.Value.Valid(fl.BaseType) {
				continue
			}
			v, ok := intOf(fl.Value)
			switch {
			case !ok:
			case fl.Num == 253:
				at, hasAt = v, true
			case fl.Num == 0:
				pct, hasPct = v, true
			case fl.Num == 3:
				gain = v
			}
		}
		if hasAt && hasPct {
			f.Solar = append(f.Solar, Solar{At: time.Unix(at+631065600, 0).UTC(), Percent: float64(pct) / 100, GainMS: gain})
		}
		return
	}
	if !docs && m.Num != 526 && m.Num != 173 && m.Num != 114 && m.Num != 217 {
		return
	}
	n := fitnamed.FromProto(m)
	switch m.Num {
	case 526:
		g := GearItem{UUID: text(n, "uuid"), Type: integer(n, "gear_type"), Name: strings.TrimSpace(text(n, "name")), Brand: text(n, "brand"),
			Model: text(n, "model"), MaxUse: integer(n, "max_use_threshold"), UsageTracked: integer(n, "usage_tracking") == 1}
		if g.UUID != "" || g.Name != "" {
			f.Gear = append(f.Gear, g)
		}
		return
	case 173:
		c := Club{Index: len(f.Clubs), Type: integer(n, "club_type"), Name: text(n, "club_nickname"), AvgDistance: number(n, "average_distance"),
			MaxDistance: number(n, "max_distance"), Loft: number(n, "loft"), Lie: number(n, "lie")}
		f.Clubs = append(f.Clubs, c)
		return
	case 114:
		if at, ok := when(n, "timestamp"); ok {
			f.Records = append(f.Records, Record{At: at, Metric: integer(n, "metric"), Sport: text(n, "sport"), Distance: number(n, "distance"),
				Value: integer(n, "record")})
		}
		return
	case 217:
		f.change(m, n)
		return
	}
	if skip[m.Num] {
		return
	}
	if f.Doc == nil {
		f.Doc = Doc{}
	}
	f.Doc[n.Name] = append(f.Doc[n.Name], n.Map())
}

// change reads a settings change log entry: the message and field it names, in words.
func (f *File) change(m *proto.Message, n fitnamed.Message) {
	at, ok := when(n, "timestamp")
	if !ok {
		return
	}
	msg, field := integer(n, "message_number"), integer(n, "field_number")
	c := Change{At: at, Kind: text(n, "type_of_change"), Sport: text(n, "sport")}
	c.Message = fitprofile.MessageName(uint16(msg))
	c.Field = fitprofile.FieldName(uint16(msg), uint8(field))
	if v, ok := n.Get("text_value"); ok {
		c.Value = toText(v)
	} else if v, ok := n.Get("num_value"); ok {
		c.Value = valueOf(uint16(msg), uint8(field), v)
	}
	f.Changes = append(f.Changes, c)
}

// valueOf renders a changed setting's number as the field it belongs to means it: an
// enumeration's name, a scaled number, else the number.
func valueOf(msg uint16, field uint8, v any) string {
	raw, ok := v.(int64)
	if !ok {
		return toText(v)
	}
	pm, _ := fitprofile.Lookup(msg)
	pf, ok := pm.Field(field)
	if !ok {
		return strconv.FormatInt(raw, 10)
	}
	if e, ok := fitprofile.LookupEnum(pf.Type); ok {
		if name := e.Value(uint32(raw)); name != "" {
			return name
		}
	}
	if pf.Type == "file" && raw >= 0 && raw < 256 {
		return fitprofile.FileTypeName(uint8(raw))
	}
	if pf.Scale > 1 || pf.Offset != 0 {
		out := strconv.FormatFloat(pf.Value(float64(raw)), 'g', 8, 64)
		if pf.Units != "" {
			out += " " + pf.Units
		}
		return out
	}
	if pf.Units != "" {
		return strconv.FormatInt(raw, 10) + " " + pf.Units
	}
	return strconv.FormatInt(raw, 10)
}

// SortSolar orders solar points by time (files list them in order; this keeps it so).
func SortSolar(s []Solar) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].At.Before(s[j].At) })
}

// intOf is an integer value as int64.
func intOf(v proto.Value) (int64, bool) {
	switch v.Type() {
	case proto.TypeUint8:
		return int64(v.Uint8()), true
	case proto.TypeUint16:
		return int64(v.Uint16()), true
	case proto.TypeUint32:
		return int64(v.Uint32()), true
	case proto.TypeInt8:
		return int64(v.Int8()), true
	case proto.TypeInt16:
		return int64(v.Int16()), true
	case proto.TypeInt32:
		return int64(v.Int32()), true
	}
	return 0, false
}

func text(n fitnamed.Message, name string) string {
	if v, ok := n.Get(name); ok {
		return toText(v)
	}
	return ""
}

func toText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	return strings.TrimSpace(strings.Trim(strings.ReplaceAll(fmt.Sprint(v), "\x00", ""), "[]"))
}

func integer(n fitnamed.Message, name string) int64 {
	v, _ := n.Get(name)
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	}
	for _, f := range n.Fields { // an enumeration shown by name: its stored number
		if f.Name == name {
			if r, ok := f.Raw.(int64); ok {
				return r
			}
		}
	}
	return 0
}

func number(n fitnamed.Message, name string) float64 {
	v, _ := n.Get(name)
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

func when(n fitnamed.Message, name string) (time.Time, bool) {
	v, ok := n.Get(name)
	if s, isText := v.(string); ok && isText {
		t, err := time.Parse(time.RFC3339, s)
		return t, err == nil
	}
	return time.Time{}, false
}
