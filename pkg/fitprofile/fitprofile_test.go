// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitprofile

import (
	"math"
	"testing"
)

func TestOfficialMessages(t *testing.T) {
	m, ok := Lookup(0)
	if !ok || m.Name != "file_id" || !m.Official {
		t.Fatalf("message 0: %+v", m)
	}
	if f, ok := m.Field(0); !ok || f.Name != "type" || f.Type != "file" || !f.Official {
		t.Fatalf("file_id.type: %+v", f)
	}
	rec, _ := Lookup(20)
	hr, _ := rec.Field(3)
	alt, _ := rec.Field(2)
	if rec.Name != "record" || hr.Name != "heart_rate" || hr.Units != "bpm" || alt.Name != "altitude" || alt.Scale != 5 || alt.Offset != 500 {
		t.Fatalf("record: %+v %+v", hr, alt)
	}
	// 3000 stored is 3000/5 − 500 = 100 m.
	if v := alt.Value(3000); math.Abs(v-100) > 1e-9 {
		t.Fatalf("altitude value %v", v)
	}
	if v := hr.Value(72); v != 72 {
		t.Fatalf("a field without scale changed its value: %v", v)
	}
}

func TestCommunityMessages(t *testing.T) {
	for num, name := range map[uint16]string{318: "solar_charge", 336: "ecg_summary", 337: "ecg_raw_sample", 338: "ecg_smooth_sample",
		397: "skin_temp_raw", 140: "physiological_metrics", 79: "user_metrics", 217: "settings_changelog"} {
		m, ok := Lookup(num)
		if !ok || m.Name != name || m.Official {
			t.Errorf("message %d: %+v, want community %q", num, m, name)
		}
		if len(m.Fields) == 0 {
			t.Errorf("message %d has no fields", num)
		}
	}
	raw, _ := Lookup(397)
	if f, ok := raw.Field(1); !ok || f.Name != "deviation" || f.Official {
		t.Fatalf("skin_temp_raw.deviation: %+v", f)
	}
	// An official message keeps its official fields and gains the community's.
	night, _ := Lookup(398)
	if !night.Official {
		t.Fatal("skin_temp_overnight is official")
	}
	if f, _ := night.Field(4); f.Name != "nightly_value" || !f.Official {
		t.Fatalf("official field: %+v", f)
	}
	if f, _ := night.Field(3); f.Name != "calibrated_days" || f.Official {
		t.Fatalf("community field in an official message: %+v", f)
	}
	if MessageName(318) != "solar_charge" || MessageName(64999) != "message_64999" || FieldName(20, 3) != "heart_rate" || FieldName(20, 254) == "" {
		t.Fatal("names")
	}
	if FieldName(64999, 1) != "field_1" {
		t.Fatal(FieldName(64999, 1))
	}
}

func TestUnitsAreSymbols(t *testing.T) {
	for _, m := range Messages() {
		for _, f := range m.Fields {
			if len(f.Units) > 3 && f.Units == upper(f.Units) && f.Units != "OTUs" {
				t.Errorf("%s.%s: unit %q is a name, not a symbol", m.Name, f.Name, f.Units)
			}
		}
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func TestTablesSorted(t *testing.T) {
	ms := Messages()
	if len(ms) < 200 {
		t.Fatalf("only %d messages", len(ms))
	}
	official := 0
	for i, m := range ms {
		if i > 0 && ms[i-1].Num >= m.Num {
			t.Fatalf("messages out of order at %d", m.Num)
		}
		if m.Official {
			official++
		}
		for j := 1; j < len(m.Fields); j++ {
			if m.Fields[j-1].Num >= m.Fields[j].Num {
				t.Fatalf("%s: fields out of order at %d", m.Name, m.Fields[j].Num)
			}
		}
		if m.Name == "" {
			t.Fatalf("message %d has no name", m.Num)
		}
	}
	if official < 120 {
		t.Fatalf("only %d official messages", official)
	}
	ft := FileTypes()
	for i := 1; i < len(ft); i++ {
		if ft[i-1].Num >= ft[i].Num {
			t.Fatalf("file types out of order at %d", ft[i].Num)
		}
	}
	ds := Devices()
	for i := 1; i < len(ds); i++ {
		a, b := ds[i-1], ds[i]
		if a.Manufacturer > b.Manufacturer || (a.Manufacturer == b.Manufacturer && a.Product >= b.Product) {
			t.Fatalf("devices out of order at %d/%d", b.Manufacturer, b.Product)
		}
	}
}

func TestEnums(t *testing.T) {
	if EnumValue("sleep_level", 4) == "" || EnumValue("sleep_level", 1) == "" {
		t.Fatalf("sleep_level: %q %q", EnumValue("sleep_level", 4), EnumValue("sleep_level", 1))
	}
	if _, ok := LookupEnum("activity_type"); !ok {
		t.Fatal("activity_type (the community spells it activitiy_type)")
	}
	if _, ok := LookupEnum("hrv_status"); !ok {
		t.Fatal("hrv_status")
	}
	if EnumValue("sleep_level", 999) != "" || EnumValue("no_such_enum", 1) != "" {
		t.Fatal("unknown values must be empty")
	}
	if len(Enums()) < 80 {
		t.Fatalf("%d enums", len(Enums()))
	}
}

func TestFileTypes(t *testing.T) {
	for num, want := range map[uint8][2]string{
		4: {"activity", "Activity"}, 9: {"weight", "Weight"}, 32: {"monitoring_b", "Monitoring"}, 44: {"metrics", "Training metrics"},
		49: {"sleep", "Sleep"}, 61: {"ecg", "ECG"}, 68: {"hrv_status", "HRV status"}, 73: {"skin_temp", "Skin temperature"},
		79: {"slp_disr", "Sleep disruption"}, 41: {"changelog", "Settings change log"},
	} {
		ft, ok := LookupFileType(num)
		if !ok || ft.Name != want[0] || ft.Title != want[1] {
			t.Errorf("file type %d: %+v, want %v", num, ft, want)
		}
	}
	if ft, _ := LookupFileType(4); !ft.Official || !ft.Archive {
		t.Fatalf("activity: %+v", ft)
	}
	if ft, _ := LookupFileType(73); ft.Official {
		t.Fatal("skin_temp is not in Garmin's published profile")
	}
	var unknown uint8
	for n := 255; n >= 0; n-- {
		if _, ok := LookupFileType(uint8(n)); !ok {
			unknown = uint8(n)
			break
		}
	}
	if FileTypeName(unknown) == "" || FileTypeTitle(unknown) == "" {
		t.Fatal("unknown file types need a fallback name")
	}
	if OtherFileName(255, 245) != "error_shutdown_reports" || OtherFileName(1, 1) != "" {
		t.Fatalf("other files: %q", OtherFileName(255, 245))
	}
}

func TestDevices(t *testing.T) {
	for p, want := range map[uint16]string{3461: "Index Smart Scale 2", 3888: "Instinct 2", 4536: "Fenix 8"} {
		if got := DeviceName(1, p); got != want {
			t.Errorf("device %d: %q, want %q", p, got, want)
		}
	}
	if DeviceName(1, 65534) == "" {
		t.Fatal("65534 is what files made by Garmin Connect name as their product")
	}
	for p := 60000; p < 65534; p++ {
		known := false
		for _, d := range Devices() {
			if d.Manufacturer == 1 && int(d.Product) == p {
				known = true
				break
			}
		}
		if !known {
			if DeviceName(1, uint16(p)) != "" {
				t.Fatalf("unknown product %d has a name", p)
			}
			return
		}
	}
}
