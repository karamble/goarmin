// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

// TestReaderRoutes: a weighing, an ECG and gear come in with an import; the body, ECG and gear
// routes serve them, the file list and its inspector name what each file holds, and another
// account sees none of it.
func TestReaderRoutes(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	at := time.Now().Add(-26 * time.Hour).Truncate(time.Second)
	ts := proto.Uint32(fittest.FITTime(at))
	serial := uint32(3_600_000_000 + u.ID)
	files := map[string][]byte{
		"W.fit": fittest.Build(typedef.File(9), serial, at,
			fittest.Message(30, fittest.F{Num: 253, Value: ts}, fittest.F{Num: 0, Value: proto.Uint16(7520)}, fittest.F{Num: 13, Value: proto.Uint16(231)})),
		"E.fit": fittest.Build(typedef.File(61), serial, at,
			fittest.Message(336, fittest.F{Num: 4, Value: ts}, fittest.F{Num: 1, Value: proto.Uint8(1)}, fittest.F{Num: 12, Value: proto.Uint8(40)}),
			fittest.Message(337, fittest.F{Num: 0, Value: proto.Float32(12)}),
			fittest.Message(337, fittest.F{Num: 0, Value: proto.Float32(-4)})),
		"G.fit": fittest.Build(typedef.File(87), serial, at,
			fittest.Message(526, fittest.F{Num: 0, Value: proto.String("u-1")}, fittest.F{Num: 2, Value: proto.String("Shoes")})),
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rep, err := e.in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil || rep.Ingested != len(files) {
		t.Fatalf("import %+v %v", rep, err)
	}
	day := at.In(time.Local).Format(time.DateOnly)
	q := "?from=" + at.AddDate(0, 0, -1).Format(time.DateOnly) + "&to=" + at.AddDate(0, 0, 1).Format(time.DateOnly)
	body := decodeJSON[map[string][]map[string]any](t, admin.want(200, "GET", "/api/body"+q, nil))
	if w := body["weigh_ins"]; len(w) != 1 || w[0]["weight_kg"] != 75.2 || w[0]["bmi"] != 23.1 || w[0]["day"] != day {
		t.Fatalf("body %v", body)
	}
	ecgs := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/ecg", nil))
	if len(ecgs) != 1 || ecgs[0]["rmssd_ms"] != float64(40) {
		t.Fatalf("ecgs %v", ecgs)
	}
	id := fmt.Sprint(ecgs[0]["id"])
	one := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/ecg/"+id, nil))
	if raw := one["samples"].(map[string]any)["raw"].([]any); len(raw) != 2 || raw[1] != float64(-4) {
		t.Fatalf("ecg samples %v", one["samples"])
	}
	gear := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/gear", nil))
	if len(gear) != 1 || gear[0]["name"] != "Shoes" || gear[0]["source"] != "fit" {
		t.Fatalf("gear %v", gear)
	}

	list := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/files?type=9", nil))
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["type_name"] != "Weight" {
		t.Fatalf("files %v", list)
	}
	fid := fmt.Sprint(items[0].(map[string]any)["id"])
	msgs := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/files/"+fid+"/messages?num=30", nil))
	shown := msgs["messages"].([]any)
	if len(shown) != 1 || msgs["total"] != float64(1) {
		t.Fatalf("messages %v", msgs)
	}
	m := shown[0].(map[string]any)
	if m["name"] != "weight_scale" || !strings.Contains(fmt.Sprint(m["fields"]), "75.2") {
		t.Fatalf("weight_scale %v", m)
	}
	if counts := msgs["counts"].([]any); len(counts) < 2 {
		t.Fatalf("counts %v", counts)
	}
	admin.want(200, "GET", "/api/files/"+fid+"/download", nil)

	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	b := e.client()
	b.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password})
	b.want(404, "GET", "/api/files/"+fid+"/messages", nil)
	b.want(404, "GET", "/api/files/"+fid+"/download", nil)
	b.want(404, "GET", "/api/ecg/"+id, nil)
	if other := decodeJSON[map[string][]map[string]any](t, b.want(200, "GET", "/api/body"+q, nil)); len(other["weigh_ins"]) != 0 {
		t.Fatalf("another account's body %v", other)
	}
	if other := decodeJSON[map[string]any](t, b.want(200, "GET", "/api/files", nil)); len(other["items"].([]any)) != 0 {
		t.Fatalf("another account's files %v", other)
	}
}
