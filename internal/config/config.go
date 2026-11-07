// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package config resolves goarmin's data directory and reads GOARMIN_* environment variables.
// Every command-line flag takes its default from an environment variable, so the same settings
// work from a shell, a systemd unit or a container. There are no config files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DataDir returns the directory holding goarmin's database, raw files and caches:
// $GOARMIN_DATA_DIR, else $XDG_DATA_HOME/goarmin, else ~/.local/share/goarmin. It is always
// outside the source tree.
func DataDir() string {
	if d := os.Getenv("GOARMIN_DATA_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "goarmin")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".goarmin"
	}
	return filepath.Join(home, ".local", "share", "goarmin")
}

// String returns the environment variable key, or def when it is unset or empty.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Int returns the integer in key, def when unset, or an error naming the variable when the
// value does not parse.
func Int(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("%s=%q: not an integer", key, v)
	}
	return n, nil
}

// Bool returns the boolean in key (1/0, true/false, yes/no, on/off), def when unset, or an
// error naming the variable.
func Bool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	switch v {
	case "":
		return def, nil
	case "1", "true", "TRUE", "True", "yes", "on":
		return true, nil
	case "0", "false", "FALSE", "False", "no", "off":
		return false, nil
	}
	return def, fmt.Errorf("%s=%q: not a boolean", key, v)
}

// Duration returns the Go duration in key (e.g. "30m"), def when unset, or an error naming
// the variable.
func Duration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, fmt.Errorf("%s=%q: not a duration", key, v)
	}
	return d, nil
}
