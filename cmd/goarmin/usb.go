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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/usbsource"
	"github.com/karamble/goarmin/pkg/mtp"
)

func runUSB(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("usb", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", config.DataDir(), "data directory (for usb.lock, shared with the server)")
	dest := fs.String("copy", "", "copy the watch's GARMIN folder into this folder")
	device := fs.String("device", "", "which device: its USB port (3-8), serial or vendor:product; empty = the only Garmin on USB")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin usb [-copy DIR] [-device ID]\n\nLists the watches (and other MTP devices) on USB and whether goarmin can read them. With\n-copy it copies the watch's GARMIN folder into DIR, only reading: nothing on the watch\nchanges. Files already in DIR with the same size and time are not read again.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	devs, err := mtp.Devices()
	if err != nil {
		fmt.Fprintln(stderr, "goarmin usb:", err)
		return 1
	}
	unlock, err := usbsource.Lock(*dataDir)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin usb:", err)
		return 1
	}
	defer unlock()
	if *dest == "" {
		return usbList(ctx, devs, stdout)
	}
	d, err := pickUSB(devs, *device)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin usb:", err)
		return 1
	}
	return usbCopy(ctx, d, *dest, stdout, stderr)
}

func usbList(ctx context.Context, devs []mtp.Info, stdout io.Writer) int {
	if len(devs) == 0 {
		fmt.Fprintln(stdout, "No MTP device on USB. Plug the watch in with its cable; it shows up within a few seconds.")
		return 0
	}
	for _, d := range devs {
		name := strings.TrimSpace(d.Manufacturer + " " + d.ProductName)
		if name == "" {
			name = "(no name)"
		}
		kind := ""
		if usbsource.IsGarmin(d) {
			kind = "  Garmin"
		}
		fmt.Fprintf(stdout, "%s  %s  %s  serial %q  %s%s\n", d.Path, d.ID(), name, d.Serial, d.DevNode, kind)
		s, err := mtp.Open(ctx, d, 0)
		if err != nil {
			fmt.Fprintf(stdout, "    cannot read it: %v\n", err)
			if h := usbsource.Hint(err); h != "" {
				fmt.Fprintf(stdout, "    %s\n", h)
			}
			continue
		}
		fmt.Fprintf(stdout, "    %s %s, version %s, serial %q\n", s.Info.Manufacturer, s.Info.Model, s.Info.DeviceVersion, s.Info.SerialNumber)
		ids, err := s.StorageIDs(ctx)
		if err != nil {
			fmt.Fprintf(stdout, "    storages: %v\n", err)
		}
		for _, id := range ids {
			if si, err := s.StorageInfo(ctx, id); err == nil {
				fmt.Fprintf(stdout, "    storage %q: %s free of %s\n", si.Description, byteSize(int64(si.FreeBytes)), byteSize(int64(si.MaxCapacity)))
			}
		}
		var folders []string
		err = s.Walk(ctx, "GARMIN", func(e mtp.Entry) error {
			if e.Folder {
				folders = append(folders, e.Name)
				return fs.SkipDir
			}
			return nil
		})
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "    GARMIN folder: %v\n", err)
		case folders == nil:
			fmt.Fprintln(stdout, "    no GARMIN folder")
		default:
			fmt.Fprintf(stdout, "    GARMIN: %s\n", strings.Join(folders, ", "))
		}
		s.Close()
	}
	return 0
}

// pickUSB chooses the device: by port, serial or id when asked, else the only Garmin.
func pickUSB(devs []mtp.Info, want string) (mtp.Info, error) {
	var found []mtp.Info
	for _, d := range devs {
		if want == "" && usbsource.IsGarmin(d) || want != "" && (d.Path == want || d.Serial == want || d.ID() == strings.ToLower(want)) {
			found = append(found, d)
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) == 0 && want == "":
		return mtp.Info{}, errors.New("no Garmin watch on USB (goarmin usb lists what is there)")
	case len(found) == 0:
		return mtp.Info{}, fmt.Errorf("no MTP device %q on USB", want)
	}
	return mtp.Info{}, errors.New("more than one device matches; choose one with -device (its port, as goarmin usb lists them)")
}

func usbCopy(ctx context.Context, d mtp.Info, dest string, stdout, stderr io.Writer) int {
	s, err := mtp.Open(ctx, d, 0)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin usb:", err)
		if h := usbsource.Hint(err); h != "" {
			fmt.Fprintln(stderr, h)
		}
		return 1
	}
	defer s.Close()
	start := time.Now()
	var files, copied, same, failed int
	var bytes int64
	err = s.Walk(ctx, "GARMIN", func(e mtp.Entry) error {
		if e.Folder {
			return nil
		}
		files++
		to := filepath.Join(dest, filepath.FromSlash(e.Path))
		if st, err := os.Stat(to); err == nil && st.Size() == e.Size && (e.Modified.IsZero() || st.ModTime().Equal(e.Modified)) {
			same++
			return nil
		}
		n, err := copyEntry(ctx, s, e, to)
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "goarmin usb: %s: %v\n", e.Path, err)
			if errors.Is(err, mtp.ErrProtocol) || errors.Is(err, mtp.ErrTimeout) || errors.Is(err, mtp.ErrGone) || ctx.Err() != nil {
				return err // the session cannot go on
			}
			return nil
		}
		copied++
		bytes += n
		return nil
	})
	fmt.Fprintf(stdout, "%s %s: %d files, %d copied (%s), %d unchanged, %d failed (%s)\n",
		s.Info.Manufacturer, s.Info.Model, files, copied, byteSize(bytes), same, failed, time.Since(start).Round(time.Millisecond))
	if err != nil {
		fmt.Fprintln(stderr, "goarmin usb:", err)
		return 1
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// copyEntry downloads one file next to its destination, then renames it into place with the
// watch's time.
func copyEntry(ctx context.Context, s *mtp.Session, e mtp.Entry, to string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return 0, err
	}
	f, err := os.CreateTemp(filepath.Dir(to), "."+filepath.Base(to)+".*.part")
	if err != nil {
		return 0, err
	}
	tmp := f.Name()
	n, err := s.Get(ctx, e.Handle, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && e.Size >= 0 && n != e.Size {
		err = fmt.Errorf("got %d bytes, the watch lists %d", n, e.Size)
	}
	if err == nil && !e.Modified.IsZero() {
		err = os.Chtimes(tmp, e.Modified, e.Modified)
	}
	if err == nil {
		err = os.Rename(tmp, to)
	}
	if err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return n, nil
}
