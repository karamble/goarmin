// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// Characteristic describes a GATT characteristic of a connected device.
type Characteristic struct {
	Path        dbus.ObjectPath
	ServicePath dbus.ObjectPath
	ServiceUUID string
	UUID        string
	Flags       []string
}

// Has reports whether the characteristic advertises a flag such as "notify" or
// "write-without-response".
func (ch Characteristic) Has(flag string) bool { return slices.Contains(ch.Flags, flag) }

// Characteristics lists the resolved GATT characteristics of d, each with its service UUID.
// Characteristics must be looked up per service: Garmin watches reuse characteristic UUIDs
// across services.
func (c *Client) Characteristics(ctx context.Context, d Device) ([]Characteristic, error) {
	objs, err := c.managedObjects(ctx)
	if err != nil {
		return nil, err
	}
	prefix := string(d.Path) + "/"
	services := map[dbus.ObjectPath]string{}
	for path, ifaces := range objs {
		if p, ok := ifaces[ifaceGattSvc]; ok && strings.HasPrefix(string(path), prefix) {
			if u, ok := p["UUID"].Value().(string); ok {
				services[path] = strings.ToLower(u)
			}
		}
	}
	var out []Characteristic
	for path, ifaces := range objs {
		p, ok := ifaces[ifaceGattChar]
		if !ok || !strings.HasPrefix(string(path), prefix) {
			continue
		}
		ch := Characteristic{Path: path}
		if u, ok := p["UUID"].Value().(string); ok {
			ch.UUID = strings.ToLower(u)
		}
		ch.ServicePath, _ = p["Service"].Value().(dbus.ObjectPath)
		ch.ServiceUUID = services[ch.ServicePath]
		ch.Flags, _ = p["Flags"].Value().([]string)
		out = append(out, ch)
	}
	slices.SortFunc(out, func(a, b Characteristic) int { return strings.Compare(string(a.Path), string(b.Path)) })
	return out, nil
}

// Link is a packet link over one GATT characteristic pair: values notified on RX arrive on
// Recv in order, and Send writes one value to TX.
type Link struct {
	c        *Client
	rx, tx   Characteristic
	maxWrite int
	mode     string

	notifyFile *os.File // from AcquireNotify; nil in D-Bus fallback mode
	writeFile  *os.File // from AcquireWrite; nil in D-Bus fallback mode
	sigCh      chan *dbus.Signal
	writeType  string

	recv chan []byte
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
	wg   sync.WaitGroup
}

// OpenLink starts notifications on rx and prepares writes to tx. It prefers BlueZ's
// AcquireNotify/AcquireWrite sockets, which deliver notifications in order without one D-Bus
// message per packet, and falls back to StartNotify and WriteValue.
func (c *Client) OpenLink(ctx context.Context, rx, tx Characteristic) (*Link, error) {
	l := &Link{c: c, rx: rx, tx: tx, recv: make(chan []byte, 256), done: make(chan struct{}), maxWrite: 20}
	notifyMode, writeMode := "dbus", "dbus"

	var fd dbus.UnixFD
	var mtu uint16
	if err := c.call(ctx, rx.Path, ifaceGattChar+".AcquireNotify", map[string]dbus.Variant{}).Store(&fd, &mtu); err == nil {
		f, ferr := pollableFile(int(fd), "notify")
		if ferr != nil {
			return nil, ferr
		}
		l.notifyFile, notifyMode = f, "acquired"
		l.setMTU(mtu)
	} else {
		if err := l.startSignalNotify(ctx); err != nil {
			return nil, err
		}
	}

	if tx.Has("write-without-response") {
		var wfd dbus.UnixFD
		var wmtu uint16
		if err := c.call(ctx, tx.Path, ifaceGattChar+".AcquireWrite", map[string]dbus.Variant{}).Store(&wfd, &wmtu); err == nil {
			f, ferr := pollableFile(int(wfd), "write")
			if ferr != nil {
				l.Close()
				return nil, ferr
			}
			l.writeFile, writeMode = f, "acquired"
			l.setMTU(wmtu)
		}
		l.writeType = "command"
	} else {
		l.writeType = "request"
	}
	if l.writeFile == nil {
		if v, err := c.getProperty(ctx, tx.Path, ifaceGattChar, "MTU"); err == nil {
			if m, ok := v.Value().(uint16); ok {
				l.setMTU(m)
			}
		}
	}
	l.mode = "notify=" + notifyMode + " write=" + writeMode + "/" + l.writeType

	if l.notifyFile != nil {
		l.wg.Add(1)
		go l.readSocket()
	}
	return l, nil
}

// setMTU derives the maximum write payload from an ATT MTU: MTU - 3, at most 512.
func (l *Link) setMTU(mtu uint16) {
	if mtu > 23 {
		l.maxWrite = min(512, int(mtu)-3)
	}
}

// pollableFile wraps an acquired socket so that Close interrupts a blocked Read.
func pollableFile(fd int, name string) (*os.File, error) {
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("bluez: set %s socket non-blocking: %w", name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (l *Link) readSocket() {
	defer l.wg.Done()
	buf := make([]byte, 1024)
	for {
		n, err := l.notifyFile.Read(buf)
		if n > 0 {
			select {
			case l.recv <- append([]byte(nil), buf[:n]...):
			case <-l.done:
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("bluez: notification socket closed (device disconnected?)")
			}
			l.fail(err)
			return
		}
	}
}

func (l *Link) startSignalNotify(ctx context.Context) error {
	l.sigCh = make(chan *dbus.Signal, 256)
	if err := l.c.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(l.rx.Path),
		dbus.WithMatchInterface(ifaceProps),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		return fmt.Errorf("bluez: add signal match: %w", err)
	}
	l.c.conn.Signal(l.sigCh)
	if err := l.c.call(ctx, l.rx.Path, ifaceGattChar+".StartNotify").Err; err != nil {
		return fmt.Errorf("bluez: StartNotify: %w", err)
	}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		for {
			select {
			case <-l.done:
				return
			case s := <-l.sigCh:
				if s == nil || s.Path != l.rx.Path || len(s.Body) < 2 {
					continue
				}
				changed, _ := s.Body[1].(map[string]dbus.Variant)
				if v, ok := changed["Value"]; ok {
					if b, ok := v.Value().([]byte); ok {
						select {
						case l.recv <- append([]byte(nil), b...):
						case <-l.done:
							return
						}
					}
				}
			}
		}
	}()
	return nil
}

// Send writes one value to TX. It must not exceed MaxWrite bytes.
func (l *Link) Send(ctx context.Context, pkt []byte) error {
	if len(pkt) > l.maxWrite {
		return fmt.Errorf("bluez: %d-byte write exceeds the %d-byte limit", len(pkt), l.maxWrite)
	}
	select {
	case <-l.done:
		return l.Err()
	default:
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writeFile != nil {
		if _, err := l.writeFile.Write(pkt); err != nil {
			l.fail(err)
			return err
		}
		return nil
	}
	opts := map[string]dbus.Variant{"type": dbus.MakeVariant(l.writeType)}
	return l.c.call(ctx, l.tx.Path, ifaceGattChar+".WriteValue", pkt, opts).Err
}

// Recv delivers notified values in order.
func (l *Link) Recv() <-chan []byte { return l.recv }

// MaxWrite is the largest value Send accepts.
func (l *Link) MaxWrite() int { return l.maxWrite }

// Mode describes how notifications and writes are carried, for logging.
func (l *Link) Mode() string { return l.mode }

// Done is closed when the link fails or is closed.
func (l *Link) Done() <-chan struct{} { return l.done }

// Err returns why the link ended, or nil while it is open or after a clean Close.
func (l *Link) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *Link) fail(err error) {
	l.once.Do(func() {
		l.mu.Lock()
		l.err = err
		l.mu.Unlock()
		close(l.done)
	})
}

// Close stops notifications and releases the acquired sockets.
func (l *Link) Close() error {
	l.once.Do(func() { close(l.done) })
	if l.notifyFile != nil {
		l.notifyFile.Close()
	}
	if l.writeFile != nil {
		l.writeFile.Close()
	}
	if l.sigCh != nil {
		_ = l.c.conn.Object(service, l.rx.Path).Call(ifaceGattChar+".StopNotify", 0).Err
		l.c.conn.RemoveSignal(l.sigCh)
		_ = l.c.conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(l.rx.Path),
			dbus.WithMatchInterface(ifaceProps),
			dbus.WithMatchMember("PropertiesChanged"),
		)
	}
	l.wg.Wait()
	return nil
}
