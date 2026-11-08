// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

var le = binary.LittleEndian

// Container types (PTP over USB, the Still Image class's bulk-only transport).
const (
	typeCommand  uint16 = 1
	typeData     uint16 = 2
	typeResponse uint16 = 3
	typeEvent    uint16 = 4
)

const headerLen = 12

// The operations this package uses. All of them only read.
const (
	opGetDeviceInfo    uint16 = 0x1001
	opOpenSession      uint16 = 0x1002
	opCloseSession     uint16 = 0x1003
	opGetStorageIDs    uint16 = 0x1004
	opGetStorageInfo   uint16 = 0x1005
	opGetObjectHandles uint16 = 0x1007
	opGetObjectInfo    uint16 = 0x1008
	opGetObject        uint16 = 0x1009
)

var opNames = map[uint16]string{
	opGetDeviceInfo:    "GetDeviceInfo",
	opOpenSession:      "OpenSession",
	opCloseSession:     "CloseSession",
	opGetStorageIDs:    "GetStorageIDs",
	opGetStorageInfo:   "GetStorageInfo",
	opGetObjectHandles: "GetObjectHandles",
	opGetObjectInfo:    "GetObjectInfo",
	opGetObject:        "GetObject",
}

// Response codes (PTP 1.1 section 11, MTP 1.1 appendix F).
const (
	RespOK                    uint16 = 0x2001
	RespGeneralError          uint16 = 0x2002
	RespSessionNotOpen        uint16 = 0x2003
	RespInvalidTransactionID  uint16 = 0x2004
	RespOperationNotSupported uint16 = 0x2005
	RespParameterNotSupported uint16 = 0x2006
	RespIncompleteTransfer    uint16 = 0x2007
	RespInvalidStorageID      uint16 = 0x2008
	RespInvalidObjectHandle   uint16 = 0x2009
	RespAccessDenied          uint16 = 0x200F
	RespStoreNotAvailable     uint16 = 0x2013
	RespDeviceBusy            uint16 = 0x2019
	RespInvalidParentObject   uint16 = 0x201A
	RespInvalidParameter      uint16 = 0x201D
	RespSessionAlreadyOpen    uint16 = 0x201E
	RespTransactionCancelled  uint16 = 0x201F
)

var respNames = map[uint16]string{
	RespGeneralError:          "general error",
	RespSessionNotOpen:        "session not open",
	RespInvalidTransactionID:  "invalid transaction id",
	RespOperationNotSupported: "operation not supported",
	RespParameterNotSupported: "parameter not supported",
	RespIncompleteTransfer:    "incomplete transfer",
	RespInvalidStorageID:      "invalid storage id",
	RespInvalidObjectHandle:   "invalid object handle",
	RespAccessDenied:          "access denied",
	RespStoreNotAvailable:     "store not available",
	RespDeviceBusy:            "device busy",
	RespInvalidParentObject:   "invalid parent object",
	RespInvalidParameter:      "invalid parameter",
	RespSessionAlreadyOpen:    "session already open",
	RespTransactionCancelled:  "transaction cancelled",
}

// Object formats and association types used to tell folders from files.
const (
	FormatAssociation uint16 = 0x3001
	FormatUndefined   uint16 = 0x3000
	associationFolder uint16 = 0x0001
)

// ResponseError is a device's answer other than OK. The session stays usable after one.
type ResponseError struct {
	Op   uint16
	Code uint16
}

func (e *ResponseError) Error() string {
	op := opNames[e.Op]
	if op == "" {
		op = fmt.Sprintf("operation 0x%04X", e.Op)
	}
	name := respNames[e.Code]
	if name == "" {
		name = "response"
	}
	return fmt.Sprintf("mtp: %s: %s (0x%04X)", op, name, e.Code)
}

// Busy reports whether the device answered that it is busy; trying again later may work.
func (e *ResponseError) Busy() bool { return e.Code == RespDeviceBusy }

// ErrProtocol is a container the device should not have sent: too short, of the wrong type
// or transaction, or longer than it says. The session cannot be used after it.
var ErrProtocol = errors.New("mtp: protocol error")

// command builds a command container.
func command(op uint16, tid uint32, params ...uint32) []byte {
	b := make([]byte, headerLen+4*len(params))
	le.PutUint32(b[0:], uint32(len(b)))
	le.PutUint16(b[4:], typeCommand)
	le.PutUint16(b[6:], op)
	le.PutUint32(b[8:], tid)
	for i, p := range params {
		le.PutUint32(b[headerLen+4*i:], p)
	}
	return b
}

type header struct {
	Length uint32
	Type   uint16
	Code   uint16
	TID    uint32
}

func parseHeader(b []byte) (header, error) {
	if len(b) < headerLen {
		return header{}, fmt.Errorf("%w: a container of %d bytes", ErrProtocol, len(b))
	}
	h := header{Length: le.Uint32(b[0:]), Type: le.Uint16(b[4:]), Code: le.Uint16(b[6:]), TID: le.Uint32(b[8:])}
	if h.Length < headerLen {
		return h, fmt.Errorf("%w: container length %d", ErrProtocol, h.Length)
	}
	return h, nil
}

// dataset reads the little-endian datasets PTP uses (device info, object info, arrays).
type dataset struct {
	b   []byte
	err error
}

var errShortDataset = fmt.Errorf("%w: dataset ends early", ErrProtocol)

func (d *dataset) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if len(d.b) < n {
		d.err = errShortDataset
		d.b = nil
		return nil
	}
	v := d.b[:n]
	d.b = d.b[n:]
	return v
}

func (d *dataset) u8() uint8 {
	if v := d.take(1); v != nil {
		return v[0]
	}
	return 0
}

func (d *dataset) u16() uint16 {
	if v := d.take(2); v != nil {
		return le.Uint16(v)
	}
	return 0
}

func (d *dataset) u32() uint32 {
	if v := d.take(4); v != nil {
		return le.Uint32(v)
	}
	return 0
}

func (d *dataset) u64() uint64 {
	if v := d.take(8); v != nil {
		return le.Uint64(v)
	}
	return 0
}

// str reads a PTP string: a count of UTF-16 code units (including the closing zero) and the
// units, little-endian. Unpaired surrogates become U+FFFD.
func (d *dataset) str() string {
	n := int(d.u8())
	if n == 0 {
		return ""
	}
	v := d.take(2 * n)
	if v == nil {
		return ""
	}
	units := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		u := le.Uint16(v[2*i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}

// count reads an array length and checks the array can fit in what is left.
func (d *dataset) count(size int) int {
	n := d.u32()
	if d.err == nil && uint64(n)*uint64(size) > uint64(len(d.b)) {
		d.err = errShortDataset
		return 0
	}
	return int(n)
}

func (d *dataset) u16s() []uint16 {
	n := d.count(2)
	out := make([]uint16, n)
	for i := range out {
		out[i] = d.u16()
	}
	return out
}

func (d *dataset) u32s() []uint32 {
	n := d.count(4)
	out := make([]uint32, n)
	for i := range out {
		out[i] = d.u32()
	}
	return out
}

// DeviceInfo is what a device says about itself (GetDeviceInfo).
type DeviceInfo struct {
	StandardVersion        uint16
	VendorExtensionID      uint32
	VendorExtensionVersion uint16
	VendorExtensionDesc    string
	FunctionalMode         uint16
	Operations             []uint16
	Events                 []uint16
	Properties             []uint16
	CaptureFormats         []uint16
	PlaybackFormats        []uint16
	Manufacturer           string
	Model                  string
	DeviceVersion          string
	SerialNumber           string
}

// Supports reports whether the device lists op among its operations.
func (i DeviceInfo) Supports(op uint16) bool {
	for _, o := range i.Operations {
		if o == op {
			return true
		}
	}
	return false
}

func parseDeviceInfo(b []byte) (DeviceInfo, error) {
	d := &dataset{b: b}
	i := DeviceInfo{
		StandardVersion:        d.u16(),
		VendorExtensionID:      d.u32(),
		VendorExtensionVersion: d.u16(),
		VendorExtensionDesc:    d.str(),
		FunctionalMode:         d.u16(),
		Operations:             d.u16s(),
		Events:                 d.u16s(),
		Properties:             d.u16s(),
		CaptureFormats:         d.u16s(),
		PlaybackFormats:        d.u16s(),
		Manufacturer:           d.str(),
		Model:                  d.str(),
		DeviceVersion:          d.str(),
		SerialNumber:           d.str(),
	}
	if d.err != nil {
		return DeviceInfo{}, fmt.Errorf("mtp: device info: %w", d.err)
	}
	return i, nil
}

// StorageInfo describes one storage (GetStorageInfo).
type StorageInfo struct {
	Type           uint16
	FilesystemType uint16
	Access         uint16
	MaxCapacity    uint64
	FreeBytes      uint64
	FreeObjects    uint32
	Description    string
	VolumeLabel    string
}

func parseStorageInfo(b []byte) (StorageInfo, error) {
	d := &dataset{b: b}
	s := StorageInfo{
		Type:           d.u16(),
		FilesystemType: d.u16(),
		Access:         d.u16(),
		MaxCapacity:    d.u64(),
		FreeBytes:      d.u64(),
		FreeObjects:    d.u32(),
		Description:    d.str(),
		VolumeLabel:    d.str(),
	}
	if d.err != nil {
		return StorageInfo{}, fmt.Errorf("mtp: storage info: %w", d.err)
	}
	return s, nil
}

// ObjectInfo describes one object, a file or a folder (GetObjectInfo).
type ObjectInfo struct {
	Storage         uint32
	Format          uint16
	Protection      uint16
	Size            int64 // -1: the device reports 4 GiB or more
	Parent          uint32
	AssociationType uint16
	Name            string
	Captured        string // as the device sent it, e.g. "20060102T150405"
	ModifiedRaw     string
	Modified        time.Time // zero when the device gives no date
}

// Folder reports whether the object is a folder.
func (o ObjectInfo) Folder() bool {
	return o.Format == FormatAssociation && (o.AssociationType == associationFolder || o.AssociationType == 0)
}

func parseObjectInfo(b []byte, loc *time.Location) (ObjectInfo, error) {
	d := &dataset{b: b}
	o := ObjectInfo{Storage: d.u32(), Format: d.u16(), Protection: d.u16()}
	size := d.u32()
	o.Size = int64(size)
	if size == 0xFFFFFFFF {
		o.Size = -1
	}
	d.u16() // thumb format
	d.u32() // thumb size
	d.u32() // thumb width
	d.u32() // thumb height
	d.u32() // image width
	d.u32() // image height
	d.u32() // image bit depth
	o.Parent = d.u32()
	o.AssociationType = d.u16()
	d.u32() // association description
	d.u32() // sequence number
	o.Name = d.str()
	o.Captured = d.str()
	o.ModifiedRaw = d.str()
	// Keywords follow; nothing here needs them, and some devices leave them out.
	if d.err != nil {
		return ObjectInfo{}, fmt.Errorf("mtp: object info: %w", d.err)
	}
	o.Modified = parseDate(o.ModifiedRaw, loc)
	if o.Modified.IsZero() {
		o.Modified = parseDate(o.Captured, loc)
	}
	return o, nil
}

// parseDate reads a PTP date: "YYYYMMDDThhmmss", optionally with tenths of a second and a "Z"
// or "±hhmm" zone. Without a zone the time is in loc (the device's clock, normally local
// time). It returns the zero time for anything else.
func parseDate(s string, loc *time.Location) time.Time {
	if len(s) < 15 {
		return time.Time{}
	}
	base, rest := s[:15], s[15:]
	if strings.HasPrefix(rest, ".") {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
	}
	switch {
	case rest == "":
	case rest == "Z":
		loc = time.UTC
	case len(rest) == 5 && (rest[0] == '+' || rest[0] == '-'):
		z, err := time.Parse("-0700", rest)
		if err != nil {
			return time.Time{}
		}
		_, off := z.Zone()
		loc = time.FixedZone("", off)
	default:
		return time.Time{}
	}
	if loc == nil {
		loc = time.Local
	}
	t, err := time.ParseInLocation("20060102T150405", base, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}
