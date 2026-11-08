// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/karamble/goarmin/internal/backup"
	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/pkg/httpx"
)

// defaultBackupDir is GOARMIN_BACKUP_DIR, else ~/goarmin-backups (where make backup keeps them).
func defaultBackupDir() string {
	if d := config.String("GOARMIN_BACKUP_DIR", ""); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "goarmin-backups"
	}
	return filepath.Join(home, "goarmin-backups")
}

func runBackup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory (database and raw files)")
	out := fs.String("out", defaultBackupDir(), "directory for the archive (GOARMIN_BACKUP_DIR)")
	keepDays := fs.Int("keep-days", 0, "afterwards remove archives older than this many days (0: remove none)")
	keepMin := fs.Int("keep-min", 3, "with -keep-days, always keep this many of the newest archives")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin backup [-data DIR] [-out DIR] [-keep-days N -keep-min N]\n\nWrites goarmin_backup_YYYYMMDD_HHMMSS.tar.gz: a consistent snapshot of the database\n(safe while the server runs) with the raw files, the watch spool and node.json. make\nrestore BACKUP=… restores it.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *keepDays < 0 || *keepMin < 0 {
		if err == nil {
			fs.Usage()
		}
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	start := time.Now()
	res, err := backup.Write(ctx, *dataDir, *out, start)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin backup:", err)
		return 1
	}
	fmt.Fprintf(stdout, "backup written: %s (%.1f MB; accounts: %d, raw files: %d; %.1f s)\n", res.Path,
		float64(res.Size)/1e6, res.Accounts, res.RawFiles, time.Since(start).Seconds())
	removed, err := backup.Prune(*out, *keepDays, *keepMin, time.Now())
	for _, r := range removed {
		fmt.Fprintln(stdout, "removed", filepath.Base(r))
	}
	if err != nil {
		fmt.Fprintln(stderr, "goarmin backup: removing old archives:", err)
		return 1
	}
	return 0
}

// runHealthcheck asks the server on this machine (or in this container) for /healthz: exit 0
// when it answers 200. For container health checks, which have no curl.
func runHealthcheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", config.String("GOARMIN_LISTEN", "127.0.0.1:8642"), "the server's listen address")
	timeout := fs.Duration("timeout", 3*time.Second, "how long to wait for the answer")
	quiet := fs.Bool("q", false, "print nothing")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2
	}
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin healthcheck: -listen must be host:port")
		return 2
	}
	resp, err := httpx.NewClient(*timeout).Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		if !*quiet {
			fmt.Fprintln(stderr, "goarmin healthcheck:", err)
		}
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		if !*quiet {
			fmt.Fprintln(stderr, "goarmin healthcheck:", resp.Status)
		}
		return 1
	}
	if !*quiet {
		fmt.Fprintln(stdout, "ok")
	}
	return 0
}
