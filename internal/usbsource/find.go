// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package usbsource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/mtp"
)

// How a watch is read.
const (
	Direct = "direct" // MTP over USB, by goarmin itself
	GVfs   = "gvfs"   // through the desktop's MTP mount (a file manager holds the watch)
	Drive  = "drive"  // a watch that shows up as a USB drive (older Garmins)
)

// MaxFile bounds a file read from a watch.
const MaxFile = 64 << 20

// Watch is a Garmin watch plugged in by USB.
type Watch struct {
	// ID names it while it stays plugged in: from its serial, or where it is.
	ID     string
	Name   string // "Garmin fenix 8"
	Serial string // what it reports on USB ('' if nothing)
	Port   string // its USB port in sysfs ("3-8"); '' for a mount found on its own
	How    string
	// Mount is the GARMIN folder of a mount of it, if there is one.
	Mount string
	Info  mtp.Info
}

// File is one file on a watch.
type File struct {
	Path     string // "GARMIN/Activity/x.fit"
	Size     int64
	Modified time.Time
	Read     func() ([]byte, error)
}

// Tree is an open MTP session as Read uses it (mtp.Session).
type Tree interface {
	Walk(ctx context.Context, root string, fn func(mtp.Entry) error) error
	Get(ctx context.Context, handle uint32, w io.Writer) (int64, error)
	Close() error
}

// Finder finds watches and reads them. The zero Finder is not usable; see New.
type Finder struct {
	Devices    func() ([]mtp.Info, error)
	Open       func(ctx context.Context, info mtp.Info) (Tree, error)
	GVfsRoots  []string // folders holding GVfs mounts (/run/user/<uid>/gvfs)
	DriveRoots []string // folders holding USB drives (/run/media/<user>, /media/<user>)
}

// New returns the Finder for this computer.
func New() *Finder {
	f := &Finder{
		Devices: mtp.Devices,
		Open: func(ctx context.Context, info mtp.Info) (Tree, error) {
			return mtp.Open(ctx, info, 0)
		},
		GVfsRoots: []string{filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "gvfs")},
	}
	if user := os.Getenv("USER"); user != "" {
		f.DriveRoots = []string{filepath.Join("/run/media", user), filepath.Join("/media", user)}
	}
	return f
}

// Find lists the Garmin watches on USB, each once: a watch goarmin can read itself and that
// the desktop has mounted too is one watch, read directly with the mount to fall back on.
func (f *Finder) Find(ctx context.Context) ([]Watch, error) {
	devs, err := f.Devices()
	if err != nil {
		return nil, err
	}
	var out []Watch
	for _, d := range devs {
		if !IsGarmin(d) {
			continue
		}
		name := strings.TrimSpace(d.Manufacturer + " " + d.ProductName)
		if name == "" {
			name = "Garmin watch"
		}
		id := "usb-" + d.Path
		if d.Serial != "" {
			id = "usb-" + d.Serial
		}
		out = append(out, Watch{ID: id, Name: name, Serial: d.Serial, Port: d.Path, How: Direct, Info: d})
	}
	for _, m := range f.gvfsMounts() {
		matched := false
		for i := range out {
			if out[i].Mount == "" && m.matches(out[i].Info) {
				out[i].Mount, matched = m.garmin, true
				break
			}
		}
		if !matched {
			out = append(out, Watch{ID: "gvfs-" + short(m.garmin), Name: m.name, How: GVfs, Mount: m.garmin})
		}
	}
	for _, root := range f.DriveRoots {
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			if g := garminIn(filepath.Join(root, e.Name())); g != "" {
				out = append(out, Watch{ID: "drive-" + short(g), Name: e.Name(), How: Drive, Mount: g})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

type gvfsMount struct {
	name   string // the mount's own name, decoded
	garmin string // its GARMIN folder
}

// matches reports whether a GVfs mount is of the USB device d: GVfs names MTP mounts after the
// device ("mtp:host=Garmin_fenix_8_<serial>") or, in older versions, its bus and address
// ("mtp:host=[usb:003,007]").
func (m gvfsMount) matches(d mtp.Info) bool {
	if len(d.Serial) >= 4 && strings.Contains(m.name, d.Serial) {
		return true
	}
	return strings.Contains(m.name, fmt.Sprintf("usb:%03d,%03d", d.Bus, d.Address))
}

func (f *Finder) gvfsMounts() []gvfsMount {
	var out []gvfsMount
	for _, root := range f.GVfsRoots {
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			if !strings.HasPrefix(e.Name(), "mtp:") {
				continue
			}
			name, err := url.PathUnescape(strings.TrimPrefix(e.Name(), "mtp:host="))
			if err != nil {
				name = e.Name()
			}
			// The watch's storages are folders of the mount; GARMIN is in one of them.
			mount := filepath.Join(root, e.Name())
			storages, _ := os.ReadDir(mount)
			for _, s := range storages {
				if g := garminIn(filepath.Join(mount, s.Name())); g != "" {
					out = append(out, gvfsMount{name: name, garmin: g})
					break
				}
			}
		}
	}
	return out
}

// garminIn returns dir's GARMIN folder (any case), or "".
func garminIn(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() && strings.EqualFold(e.Name(), "GARMIN") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func short(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

// Read hands fn the files in the watch's GARMIN folder, without changing anything on the
// watch, and returns how it read them. A watch another program holds (ErrBusy) is read
// through its mount when there is one. File.Read works only during fn.
func (f *Finder) Read(ctx context.Context, w Watch, fn func(File) error) (string, error) {
	if w.How == Direct {
		t, err := f.Open(ctx, w.Info)
		if err == nil {
			defer t.Close()
			return Direct, readTree(ctx, t, fn)
		}
		if !errors.Is(err, mtp.ErrBusy) || w.Mount == "" {
			return Direct, err
		}
		w.How = GVfs
	}
	if w.Mount == "" {
		return w.How, errors.New("usbsource: the watch has no mount to read")
	}
	return w.How, readMount(ctx, w.Mount, fn)
}

func readTree(ctx context.Context, t Tree, fn func(File) error) error {
	return t.Walk(ctx, "GARMIN", func(e mtp.Entry) error {
		if e.Folder {
			return nil
		}
		h := e.Handle
		return fn(File{Path: e.Path, Size: e.Size, Modified: e.Modified, Read: func() ([]byte, error) {
			if e.Size > MaxFile {
				return nil, fmt.Errorf("larger than %d MB", MaxFile>>20)
			}
			var b bytes.Buffer
			if e.Size > 0 {
				b.Grow(int(e.Size))
			}
			if _, err := t.Get(ctx, h, &limitWriter{w: &b, left: MaxFile}); err != nil {
				return nil, err
			}
			return b.Bytes(), nil
		}})
	})
}

type limitWriter struct {
	w    io.Writer
	left int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, fmt.Errorf("larger than %d MB", MaxFile>>20)
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

func readMount(ctx context.Context, garmin string, fn func(File) error) error {
	top := filepath.Base(garmin)
	return filepath.WalkDir(garmin, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(garmin, p)
		if err != nil {
			return err
		}
		return fn(File{Path: path.Join(top, filepath.ToSlash(rel)), Size: st.Size(), Modified: st.ModTime(), Read: func() ([]byte, error) {
			if st.Size() > MaxFile {
				return nil, fmt.Errorf("larger than %d MB", MaxFile>>20)
			}
			return os.ReadFile(p)
		}})
	})
}
