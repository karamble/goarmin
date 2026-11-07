// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package protoschema

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const testProto = `syntax = "proto2";
package T;

// comment
message Inner {
    enum Kind {
        A = 0;
        B = 1;
    }
    optional Kind kind = 1;
    optional string name = 2;
}

message Outer {
    optional Inner inner = 1;
    repeated uint32 vals = 2 [packed = false];
    optional sint32 delta = 3;
    oneof choice {
        bool flag = 4;
    }
}

/* block comment */
service Ignored { rpc Nothing (Inner) returns (Outer); }
`

func loadTest(t *testing.T) *Schema {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.proto"), []byte(testProto), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRender(t *testing.T) {
	s := loadTest(t)
	// Outer{inner{kind:B name:"x"} vals:7 vals:9 delta:-2 flag:true f9:5}
	msg := []byte{
		0x0A, 0x05, 0x08, 0x01, 0x12, 0x01, 'x', // inner
		0x10, 0x07, 0x10, 0x09, // vals
		0x18, 0x03, // delta = -2 (zigzag 3)
		0x20, 0x01, // flag
		0x48, 0x05, // unknown field 9
	}
	want := []string{
		"inner {",
		"  kind: B(1)",
		`  name: "x"`,
		"}",
		"vals: 7",
		"vals: 9",
		"delta: -2",
		"flag: true",
		"f9: 5",
	}
	if got := s.Render("T.Outer", msg); !reflect.DeepEqual(got, want) {
		t.Fatalf("Render\n got  %q\n want %q", got, want)
	}
}

func TestResolveAndLookup(t *testing.T) {
	s := loadTest(t)
	if full, isEnum := s.Resolve("T.Inner", "Kind"); full != "T.Inner.Kind" || !isEnum {
		t.Errorf("Resolve(Kind) = %q enum=%v", full, isEnum)
	}
	if full, isEnum := s.Resolve("T.Outer", "Inner"); full != "T.Inner" || isEnum {
		t.Errorf("Resolve(Inner) = %q enum=%v", full, isEnum)
	}
	if full, _ := s.Resolve("T.Outer", "Missing"); full != "" {
		t.Errorf("Resolve(Missing) = %q, want empty", full)
	}
	if !s.HasMessage("T.Outer") || s.HasMessage("T.Ignored") {
		t.Error("HasMessage: services must not be parsed as messages")
	}
	if name, ok := s.FieldName("T.Outer", 4); !ok || name != "flag" {
		t.Errorf("FieldName(Outer, 4) = %q %v, want oneof field flag", name, ok)
	}
}

func TestRenderMalformedInput(t *testing.T) {
	s := loadTest(t)
	got := s.Render("T.Outer", []byte{0x0A, 0x7F, 0x01}) // length runs past the end
	if len(got) == 0 || got[len(got)-1] != "!! bad length" {
		t.Fatalf("malformed input rendered as %q", got)
	}
}

// The real Garmin schema from the Gadgetbridge clone parses and resolves the types goarmin
// depends on. Skipped when the clone is not present.
func TestGadgetbridgeSchemaLoads(t *testing.T) {
	dir := GadgetbridgeDir()
	if dir == "" {
		t.Skip("Gadgetbridge proto directory not found")
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := s.FieldName("garmin_vivomovehr.Smart", 43); !ok || name != "file_access_service" {
		t.Fatalf("Smart field 43 = %q %v, want file_access_service", name, ok)
	}
	for _, m := range []string{"GDI.Proto.FileAccess.ItemListRequest", "GDI.Proto.FileAccess.PullItemDataRequest",
		"GDI.Proto.FileAccess.ModifyFlagsRequest", "GDI.Proto.DataTypes.UUID"} {
		if !s.HasMessage(m) {
			t.Errorf("message %s not found", m)
		}
	}
	if full, isEnum := s.Resolve("GDI.Proto.FileAccess.ItemReference", "Urgency"); full != "GDI.Proto.FileAccess.ItemReference.Urgency" || !isEnum {
		t.Errorf("Resolve(Urgency) = %q enum=%v", full, isEnum)
	}
}
