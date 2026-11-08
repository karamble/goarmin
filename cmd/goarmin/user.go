// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

// passwordIn is where `goarmin user -set-password` reads the password (tests replace it).
var passwordIn io.Reader = os.Stdin

// runUser is account care for whoever runs the server: list the accounts, or set a password
// someone forgot.
func runUser(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory")
	email := fs.String("email", "", "the account")
	setPassword := fs.Bool("set-password", false, "set the account's password, read from standard input (one line), and sign it out everywhere")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin user               list the accounts\n       goarmin user -email EMAIL -set-password < file\n\nThe new password is read from standard input, never from the command line, so it shows up in\nno process list or shell history. Setting it signs the account out in every browser.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *setPassword != (*email != "") {
		fs.Usage()
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	st, err := store.Open(ctx, filepath.Join(*dataDir, "goarmin.db"))
	if err != nil {
		fmt.Fprintln(stderr, "goarmin user:", err)
		return 1
	}
	defer st.Close()
	if !*setPassword {
		return listUsers(ctx, st, stdout, stderr)
	}
	if f, ok := passwordIn.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			fmt.Fprintln(stderr, "goarmin user: pipe the password in (it would show on the screen when typed here): goarmin user -email EMAIL -set-password < file")
			return 2
		}
	}
	line, err := bufio.NewReader(io.LimitReader(passwordIn, auth.MaxPasswordLen+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, "goarmin user:", err)
		return 1
	}
	pw := strings.TrimRight(line, "\r\n")
	u, err := st.UserByEmail(ctx, *email)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(stderr, "goarmin user: no account %s (goarmin user lists them)\n", *email)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "goarmin user:", err)
		return 1
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin user:", strings.TrimPrefix(err.Error(), "auth: "))
		return 1
	}
	if err := st.SetPassword(ctx, u.ID, hash); err != nil {
		fmt.Fprintln(stderr, "goarmin user:", err)
		return 1
	}
	n, err := st.DeleteUserSessions(ctx, u.ID)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin user: the password is set, but signing out failed:", err)
		return 1
	}
	fmt.Fprintf(stdout, "New password set for %s; signed out of %d sessions.\n", u.Email, n)
	return 0
}

func listUsers(ctx context.Context, st *store.Store, stdout, stderr io.Writer) int {
	list, err := st.Users(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin user:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tNAME\tADMIN")
	for _, u := range list {
		admin := ""
		if u.IsAdmin {
			admin = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", u.Email, u.DisplayName, admin)
	}
	return boolExit(tw.Flush() == nil)
}

func boolExit(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
