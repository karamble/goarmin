// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command goarmin is a self-hosted replacement for Garmin Connect. One binary has two roles:
// "serve" runs the API, database and dashboard; "sync" runs next to the watches and pulls
// their files over Bluetooth. See docs/research.md and docs/protocol/.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `goarmin is a self-hosted replacement for Garmin Connect.

usage: goarmin <command> [flags]

commands:
  serve     run the API and the dashboard
  pair      pair a Garmin watch with this computer (interactive)
  probe     connect to a paired watch and run the protocol handshake
  sync      pull new files from a paired watch into the local spool (-once)
  live      stream live values (heart rate, steps, stress, …) from a paired watch
  import    store and ingest FIT files (a USB copy of the watch, a sync spool)
  usb       list the watches on USB, or copy one's GARMIN folder (read-only)
  reingest  extract the health data of the stored files again
  days      print the daily summaries of an account
  user      list the accounts, or set an account's password (-set-password)
  backup    write a backup archive (consistent while the server runs)
  healthcheck  exit 0 if the server here answers /healthz (container health checks)
  activities  list the activities of an account
  version   print the version
  help      show this help

Run "goarmin <command> -h" for the flags of one command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch cmd := args[0]; cmd {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "pair":
		return runPair(args[1:], stdout, stderr)
	case "probe":
		return runProbe(args[1:], stdout, stderr)
	case "sync":
		return runSync(args[1:], stdout, stderr)
	case "live":
		return runLive(args[1:], stdout, stderr)
	case "import":
		return runImport(args[1:], stdout, stderr)
	case "usb":
		return runUSB(args[1:], stdout, stderr)
	case "reingest":
		return runReingest(args[1:], stdout, stderr)
	case "activities":
		return runActivities(args[1:], stdout, stderr)
	case "days":
		return runDays(args[1:], stdout, stderr)
	case "user":
		return runUser(args[1:], stdout, stderr)
	case "backup":
		return runBackup(args[1:], stdout, stderr)
	case "healthcheck":
		return runHealthcheck(args[1:], stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, versionString())
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "goarmin: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

func versionString() string {
	s := "goarmin " + version
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, kv := range bi.Settings {
			if kv.Key == "vcs.revision" && len(kv.Value) >= 12 {
				s += " (" + kv.Value[:12] + ")"
			}
		}
	}
	return s + " " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
}
