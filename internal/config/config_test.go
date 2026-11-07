// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDataDir(t *testing.T) {
	t.Setenv("GOARMIN_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := DataDir(), filepath.Join(home, ".local", "share", "goarmin"); got != want {
		t.Errorf("default DataDir = %q, want %q", got, want)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	if got, want := DataDir(), filepath.Join(xdg, "goarmin"); got != want {
		t.Errorf("XDG DataDir = %q, want %q", got, want)
	}
	explicit := t.TempDir()
	t.Setenv("GOARMIN_DATA_DIR", explicit)
	if got := DataDir(); got != explicit {
		t.Errorf("GOARMIN_DATA_DIR DataDir = %q, want %q", got, explicit)
	}
}

func TestTypedEnv(t *testing.T) {
	t.Setenv("GOARMIN_T_STR", "")
	if got := String("GOARMIN_T_STR", "def"); got != "def" {
		t.Errorf("String unset = %q", got)
	}
	t.Setenv("GOARMIN_T_STR", "x")
	if got := String("GOARMIN_T_STR", "def"); got != "x" {
		t.Errorf("String set = %q", got)
	}

	t.Setenv("GOARMIN_T_INT", "42")
	if n, err := Int("GOARMIN_T_INT", 1); err != nil || n != 42 {
		t.Errorf("Int = %d, %v", n, err)
	}
	t.Setenv("GOARMIN_T_INT", "forty-two")
	if _, err := Int("GOARMIN_T_INT", 1); err == nil {
		t.Error("Int accepted a non-integer")
	}

	for v, want := range map[string]bool{"1": true, "on": true, "false": false, "no": false} {
		t.Setenv("GOARMIN_T_BOOL", v)
		if got, err := Bool("GOARMIN_T_BOOL", !want); err != nil || got != want {
			t.Errorf("Bool(%q) = %v, %v", v, got, err)
		}
	}
	t.Setenv("GOARMIN_T_BOOL", "maybe")
	if _, err := Bool("GOARMIN_T_BOOL", false); err == nil {
		t.Error("Bool accepted maybe")
	}

	t.Setenv("GOARMIN_T_DUR", "90s")
	if d, err := Duration("GOARMIN_T_DUR", time.Minute); err != nil || d != 90*time.Second {
		t.Errorf("Duration = %v, %v", d, err)
	}
	t.Setenv("GOARMIN_T_DUR", "soon")
	if _, err := Duration("GOARMIN_T_DUR", time.Minute); err == nil {
		t.Error("Duration accepted soon")
	}
}
