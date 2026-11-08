// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/nodeclient"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/bluez"
)

// secretIn is where node login reads the password (tests replace it).
var secretIn = os.Stdin

const nodeUsage = `usage: goarmin node login -server URL [-email EMAIL] [-password-stdin] [-name NAME]
       goarmin node status
       goarmin node logout

A sync node is this computer syncing the watches paired with it for a goarmin server
elsewhere: goarmin sync -server URL runs it. Sign it in once with node login: with -email it
asks for the account's password without showing it (or reads it from standard input with
-password-stdin); without -email it shows a code to approve in the dashboard (Devices › Sync
nodes). The computer then keeps a token, never the password.`

func runNode(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, nodeUsage)
		return 2
	}
	switch args[0] {
	case "login":
		return nodeLogin(args[1:], stdout, stderr)
	case "status":
		return nodeStatus(args[1:], stdout, stderr)
	case "logout":
		return nodeLogout(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(stdout, nodeUsage)
		return 0
	}
	fmt.Fprintln(stderr, nodeUsage)
	return 2
}

func nodeLogin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("node login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory (node.json, the spool, the node's sign-in)")
	server := fs.String("server", config.String("GOARMIN_SERVER", ""), "the server's address, e.g. http://goarmin.example:8642")
	email := fs.String("email", config.String("GOARMIN_EMAIL", ""), "the account (asks for its password); empty: approve a code in the dashboard")
	stdin := fs.Bool("password-stdin", false, "read the password from standard input (one line) instead of asking")
	name := fs.String("name", "", "the node's name in the dashboard (default: the host name)")
	fs.Usage = func() { fmt.Fprintln(stderr, nodeUsage); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2
	}
	if *server == "" {
		fs.Usage()
		return 2
	}
	host, _ := os.Hostname()
	if *name == "" {
		*name = host
	}
	c, err := nodeclient.NewClient(*server, "")
	if err != nil {
		fmt.Fprintln(stderr, "goarmin node login:", err)
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	var a nodeclient.Auth
	if *email != "" {
		var pw string
		switch {
		case *stdin:
			pw, err = readLine(secretIn, auth.MaxPasswordLen)
		case isTerminal(secretIn):
			fmt.Fprintf(stderr, "Password for %s: ", *email)
			pw, err = readSecret(secretIn, auth.MaxPasswordLen)
			fmt.Fprintln(stderr)
		default:
			fmt.Fprintln(stderr, "goarmin node login: no terminal to ask the password on; pipe it in with -password-stdin")
			return 2
		}
		if err != nil {
			fmt.Fprintln(stderr, "goarmin node login:", err)
			return 1
		}
		res, err := c.Login(ctx, nodeproto.LoginRequest{Email: *email, Password: pw, Name: *name, Hostname: host})
		if err != nil {
			fmt.Fprintln(stderr, "goarmin node login:", err)
			return 1
		}
		a = nodeclient.Auth{Server: c.Server(), NodeID: res.NodeID, Token: res.Token, Account: res.Account, Name: *name}
	} else {
		en, err := c.Enroll(ctx, nodeproto.EnrollRequest{Name: *name, Hostname: host})
		if err != nil {
			fmt.Fprintln(stderr, "goarmin node login:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Approve this computer in the dashboard: Devices › Sync nodes › Approve a code\n\n    %s\n\n(the code is valid until %s; waiting…)\n", en.Code, en.Expires.Local().Format("15:04"))
		for {
			tok, err := c.PollEnroll(ctx, en.Poll)
			if err != nil {
				fmt.Fprintln(stderr, "goarmin node login:", err)
				return 1
			}
			if tok != "" {
				a = nodeclient.Auth{Server: c.Server(), Token: tok, Name: *name}
				break
			}
			select {
			case <-ctx.Done():
				return 1
			case <-time.After(2 * time.Second):
			}
		}
	}
	if err := nodeclient.SaveAuth(*dataDir, a); err != nil {
		fmt.Fprintln(stderr, "goarmin node login:", err)
		return 1
	}
	who := a.Account
	if who == "" {
		who = "the account that approved the code"
	}
	fmt.Fprintf(stdout, "Signed in to %s as %s (node %q). Start syncing with: goarmin sync -server %s\n", a.Server, who, a.Name, a.Server)
	return 0
}

func nodeLogout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("node logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := nodeclient.RemoveAuth(*dataDir); err != nil {
		fmt.Fprintln(stderr, "goarmin node logout:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Signed out: this computer forgot its token. Revoke it in the dashboard too (Devices › Sync nodes).")
	return 0
}

func nodeStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("node status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fmt.Fprintf(stdout, "bluetooth %s\n", adapterLine(*adapter))
	a, err := nodeclient.LoadAuth(*dataDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "Not signed in to a server (goarmin node login -server URL).")
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "goarmin node status:", err)
		return 1
	}
	fmt.Fprintf(stdout, "server    %s\nnode      %s\naccount   %s\n", a.Server, a.Name, a.Account)
	c, err := nodeclient.NewClient(a.Server, a.Token)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin node status:", err)
		return 1
	}
	watches, _ := syncnode.KnownWatches(*dataDir)
	req := nodeproto.HelloRequest{Version: version, Watches: []nodeproto.Watch{}}
	host, _ := os.Hostname()
	req.Hostname = host
	addrs := make([]string, 0, len(watches))
	for addr := range watches {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		w := watches[addr]
		req.Watches = append(req.Watches, nodeproto.Watch{Address: addr, Unit: w.Unit, Product: w.Product, Name: w.Name, Firmware: w.Firmware})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hr, err := c.Hello(ctx, req)
	cfg := map[string]nodeproto.WatchConfig{}
	if err != nil {
		fmt.Fprintf(stdout, "server    not reachable: %v\n", err)
	} else {
		fmt.Fprintln(stdout, "server    reachable")
		for _, w := range hr.Watches {
			cfg[w.Address] = w
		}
	}
	for _, addr := range addrs {
		w := watches[addr]
		state := "unknown (server not reached)"
		if c, ok := cfg[addr]; ok {
			switch {
			case !c.Sync && c.Reason == nodeproto.ReasonOther:
				state = "not synced: another account's watch"
			case !c.Sync:
				state = "not synced: nobody owns it on the server (add it there)"
			default:
				state = "synced, after fetching: " + c.AfterFetch
				if c.Paused {
					state += ", scheduled syncs paused"
				}
			}
		}
		waiting := spoolCount(filepath.Join(*dataDir, "watches", fmt.Sprint(w.Unit), "files"))
		fmt.Fprintf(stdout, "watch     %s %s (unit %d): %s; %d files wait for the server\n", addr, w.Name, w.Unit, state, waiting)
	}
	if len(addrs) == 0 {
		fmt.Fprintln(stdout, "watch     none paired with this computer (goarmin pair)")
	}
	return 0
}

// adapterLine says how this computer's Bluetooth adapter is, read from BlueZ over D-Bus.
func adapterLine(name string) string {
	c, err := bluez.Open()
	if err != nil {
		return "not reachable over D-Bus: " + err.Error()
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, err := c.Adapter(ctx, name)
	if err != nil {
		return name + ": " + err.Error()
	}
	state := a.PowerState
	if state == "" {
		state = map[bool]string{true: "on", false: "off"}[a.Powered]
	}
	return fmt.Sprintf("%s (%s): %s", name, a.Address, state)
}

// spoolCount counts the files waiting in a spool directory.
func spoolCount(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && !strings.HasPrefix(d.Name(), ".") {
			n++
		}
		return nil
	})
	return n
}

// runNodeDaemon is goarmin sync -server: the node syncs until stopped.
func runNodeDaemon(dataDir, server, adapter string, noArchive, features, fakePairer, verbose bool, stdout, stderr io.Writer) int {
	a, err := nodeclient.LoadAuth(dataDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "goarmin sync: sign this computer in first: goarmin node login -server %s -email EMAIL\n", server)
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "goarmin sync:", err)
		return 1
	case server != "" && strings.TrimRight(server, "/") != strings.TrimRight(a.Server, "/"):
		fmt.Fprintf(stderr, "goarmin sync: this computer is signed in to %s, not %s (goarmin node login -server %s)\n", a.Server, server, server)
		return 1
	}
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signalContext()
	defer stop()
	fmt.Fprintf(stdout, "goarmin sync node for %s (Ctrl+C stops it)\n", a.Server)
	opt := nodeclient.Options{DataDir: dataDir, Adapter: adapter, Version: version, Logger: log, Features: features, NoArchive: noArchive}
	if fakePairer {
		log.Warn("scripted Bluetooth (-fake-pairer): made-up watches, no real syncs")
		opt.Hub = func(o *nodehub.Options) { *o = nodehub.Scripted(*o) }
	}
	err = nodeclient.Run(ctx, opt)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin sync:", err)
		return 1
	}
	return 0
}
