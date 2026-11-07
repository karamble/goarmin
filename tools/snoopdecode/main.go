// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command snoopdecode decodes Garmin watch traffic from an Android Bluetooth HCI snoop log
// (btsnoop format, H4 datalink), layer by layer:
//
//	HCI ACL -> L2CAP -> ATT -> Garmin Multi-Link (+ MLR) -> COBS -> GFDI -> Smart protobuf
//
// It prints a timeline of Multi-Link control messages and GFDI messages. With -proto it also
// decodes every Smart protobuf message field by field against the Garmin .proto files that
// Gadgetbridge ships. With -out it inflates every file pulled over a Multi-Link file pipe and
// writes it to that directory. See docs/protocol/05-fenix8-capture.md for what it found.
//
// Usage:
//
//	snoopdecode [-v] [-proto DIR] [-out DIR] <btsnoop_hci.log>
//
// Captures and pulled files contain personal data: keep them outside the repository.
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/btsnoop"
	"github.com/karamble/goarmin/pkg/cobs"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/mltrace"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/protoschema"
)

var verbose bool
var schema *protoschema.Schema
var pullDir string

// ---------------------------------------------------------------- Garmin Multi-Link

var serviceNames = map[uint16]string{
	1: "GFDI", 2: "NFC", 3: "HEALTH_SDK", 4: "REGISTRATION", 5: "CONNEXT", 6: "REALTIME_HR",
	7: "REALTIME_STEPS", 8: "REALTIME_CALORIES", 9: "REALTIME_FLOORS", 10: "REALTIME_INTENSITY",
	12: "REALTIME_HRV", 13: "REALTIME_STRESS", 15: "ECHO", 16: "REALTIME_ACCEL", 19: "REALTIME_SPO2",
	20: "REALTIME_BODY_BATTERY", 21: "REALTIME_RESPIRATION", 22: "KEEP_ALIVE", 26: "REALTIME_ACTIVE_TIME",
	0x2018: "FILE_TRANSFER_2", 0x4018: "FILE_TRANSFER_4", 0x6018: "FILE_TRANSFER_6",
	0x8018: "FILE_TRANSFER_8", 0xA018: "FILE_TRANSFER_A", 0xC018: "FILE_TRANSFER_C", 0xE018: "FILE_TRANSFER_E",
}

func svcName(s uint16) string {
	if n, ok := serviceNames[s]; ok {
		return n
	}
	return fmt.Sprintf("svc_0x%04x", s)
}

var ctrlNames = []string{"REGISTER_ML_REQ", "REGISTER_ML_RESP", "CLOSE_HANDLE_REQ", "CLOSE_HANDLE_RESP",
	"UNK_HANDLE", "CLOSE_ALL_REQ", "CLOSE_ALL_RESP", "UNK_REQ", "UNK_RESP"}

type mlHandle struct {
	service  uint16
	reliable bool
}

// mlrStream reassembles one direction of an MLR handle, accepting only in-order SNs.
type mlrStream struct {
	expect int
	dups   int
	data   int
}

type dirStream struct {
	cobs     []byte // pending COBS bytes
	frames   int
	mlr      mlrStream
	pipeLen  int
	pipeHead []byte
}

type streamKey struct {
	handle byte
	toDev  bool
}

// ---------------------------------------------------------------- COBS + GFDI

var gfdiNames = map[uint16]string{
	5000: "RESPONSE", 5002: "DOWNLOAD_REQUEST", 5003: "UPLOAD_REQUEST", 5004: "FILE_TRANSFER_DATA",
	5005: "CREATE_FILE", 5006: "DELETE_FILE", 5007: "FILTER", 5008: "SET_FILE_FLAG", 5009: "FILE_AVAILABLE",
	5011: "FIT_DEFINITION", 5012: "FIT_DATA", 5014: "WEATHER_REQUEST", 5015: "WEATHER_ALERT",
	5016: "LIVE_TRACK_TRACKING_REQUEST", 5019: "EPHEMERIS_DATA_REQUEST", 5020: "EPHEMERIS_DATA",
	5022: "CANCEL_FILE_TRANSFER", 5023: "BATTERY_STATUS", 5024: "DEVICE_INFORMATION", 5025: "LIVE_TRACK_STOP",
	5026: "DEVICE_SETTINGS", 5027: "QUEUED_DOWNLOAD", 5028: "EPHEMERIS_EPO_REQUEST", 5029: "EPHEMERIS_EPO_DATA",
	5030: "SYSTEM_EVENT", 5031: "SUPPORTED_FILE_TYPES_REQUEST", 5033: "NOTIFICATION_UPDATE",
	5034: "NOTIFICATION_CONTROL", 5035: "NOTIFICATION_DATA", 5036: "NOTIFICATION_SUBSCRIPTION",
	5037: "SYNCHRONIZATION", 5039: "FIND_MY_PHONE_REQUEST", 5040: "FIND_MY_PHONE_CANCEL", 5041: "MUSIC_CONTROL",
	5042: "MUSIC_CONTROL_CAPABILITIES", 5043: "PROTOBUF_REQUEST", 5044: "PROTOBUF_RESPONSE", 5045: "CANCEL_PROTOBUF",
	5049: "MUSIC_CONTROL_ENTITY_UPDATE", 5050: "CONFIGURATION", 5052: "CURRENT_TIME_REQUEST",
	5054: "COMPRESSED_FILE_DATA", 5101: "AUTH_NEGOTIATION", 5102: "LTK_RECONNECT", 5103: "STK_BEGIN_GENERATION",
	5104: "CONFIRM_NUMBER", 5105: "STK_RANDOM_NUMBER", 5106: "STK_GENERATION_STATUS", 5107: "LTK_KEY_DISTRIBUTION",
	5108: "SESSION_SKD_DISTRIBUTION", 5109: "SESSION_KEY_VERIFICATION", 5110: "PASSKEY_REDISPLAY",
	5111: "SECURE_SESSION", 5112: "OUT_OF_BAND_PASSKEY_DATA",
}

func gname(id uint16) string {
	if n, ok := gfdiNames[id]; ok {
		return n
	}
	return fmt.Sprintf("ID_%d", id)
}

var sysEvents = []string{"SYNC_COMPLETE", "SYNC_FAIL", "FACTORY_RESET", "PAIR_START", "PAIR_COMPLETE",
	"PAIR_FAIL", "HOST_DID_ENTER_FOREGROUND", "HOST_DID_ENTER_BACKGROUND", "SYNC_READY",
	"NEW_DOWNLOAD_AVAILABLE", "DEVICE_SOFTWARE_UPDATE", "DEVICE_DISCONNECT", "TUTORIAL_COMPLETE",
	"SETUP_WIZARD_START", "SETUP_WIZARD_COMPLETE", "SETUP_WIZARD_SKIPPED", "TIME_UPDATED", "TRIGGER_REBOOT"}

var smartNames = map[uint64]string{1: "calendar", 2: "connect_iq_http", 3: "ciq_installed_apps",
	4: "ciq_app_settings", 7: "data_transfer", 8: "device_status", 13: "core", 20: "hsa_data",
	22: "explore_sync", 27: "credentials", 30: "event_sharing", 31: "generic_item_transfer", 36: "simple_setup", 39: "ecg",
	42: "realtime_settings", 43: "file_access", 47: "garmin_connect", 49: "gncs", 61: "heart_rate"}

var fileAccessNames = map[uint64]string{1: "PullItemDataRequest", 2: "PullItemDataResponse",
	3: "PushItemDataRequest", 4: "PushItemDataResponse", 5: "TransferStatusRequest",
	6: "PriorityUpdateRequest", 7: "PriorityUpdateResponse", 9: "ItemListRequest", 10: "ItemListResponse",
	11: "ItemListCancelNotification", 12: "ItemAddedNotification", 13: "DeleteItemRequest",
	14: "DeleteItemResponse", 15: "ModifyFlagsRequest", 16: "ModifyFlagsResponse",
	17: "ItemUpdatedNotification", 18: "CancelTransferRequest", 19: "CancelTransferResponse",
	20: "ResourceUpdateNotification", 21: "TransferStatusResponse", 22: "SyncButtonNotification",
	23: "SoftwareUpdatePartNumberRequest", 24: "SoftwareUpdatePartNumberResponse",
	25: "GetItemChecksumRequest", 26: "GetItemChecksumResponse"}

// protoFields returns the top-level (field number, bytes-if-length-delimited) pairs.
func protoFields(b []byte) (out [][2]any) {
	for i := 0; i < len(b); {
		tag, n := uvarint(b[i:])
		if n <= 0 {
			return
		}
		i += n
		fn, wt := tag>>3, tag&7
		switch wt {
		case 0:
			_, n = uvarint(b[i:])
			if n <= 0 {
				return
			}
			i += n
			out = append(out, [2]any{fn, nil})
		case 1:
			i += 8
			out = append(out, [2]any{fn, nil})
		case 5:
			i += 4
			out = append(out, [2]any{fn, nil})
		case 2:
			l, n := uvarint(b[i:])
			if n <= 0 || i+n+int(l) > len(b) {
				return
			}
			i += n
			out = append(out, [2]any{fn, b[i : i+int(l)]})
			i += int(l)
		default:
			return
		}
	}
	return
}

func uvarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i == 10 {
			return 0, -1
		}
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

func describeSmart(b []byte) string {
	var parts []string
	for _, f := range protoFields(b) {
		fn := f[0].(uint64)
		name := smartNames[fn]
		if name == "" {
			name = fmt.Sprintf("f%d", fn)
		}
		inner, _ := f[1].([]byte)
		var sub []string
		for _, g := range protoFields(inner) {
			gn := g[0].(uint64)
			if fn == 43 {
				if s, ok := fileAccessNames[gn]; ok {
					sub = append(sub, s)
					continue
				}
			}
			sub = append(sub, fmt.Sprintf("f%d", gn))
		}
		parts = append(parts, fmt.Sprintf("%s{%s}", name, strings.Join(sub, ",")))
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------- analysis state

type analysis struct {
	t0        time.Time
	handles   map[byte]mlHandle
	streams   map[streamKey]*dirStream
	clientIDs map[uint64]bool
	gfdiCount map[string]int
	protoBuf  map[[2]uint64][]byte // (dir, reqId) -> reassembly
	smartSeen map[string]int
	notes     []string
	ftdCount  map[bool]int
	ftdBytes  map[bool]int
	lines     int
	pipes     map[byte]*pipeXfer
	pulled    int
}

type pipeXfer struct {
	svc      uint16
	dir      byte
	token    uint16
	rx, tx   []byte
	gotReply bool
	ts       time.Time
}

func (a *analysis) logf(ts time.Time, format string, args ...any) {
	a.lines++
	fmt.Printf("%8.3fs  %s\n", ts.Sub(a.t0).Seconds(), fmt.Sprintf(format, args...))
}

func dir(toDev bool) string {
	if toDev {
		return "H→W"
	}
	return "W→H"
}

func (a *analysis) control(v btsnoop.ATTValue) {
	b := v.Value
	if len(b) < 10 {
		a.logf(v.TS, "%s ML ctrl (short) % x", dir(v.ToDevice), b)
		return
	}
	typ := int(b[1])
	client := binary.LittleEndian.Uint64(b[2:10])
	a.clientIDs[client] = true
	name := fmt.Sprintf("type%d", typ)
	if typ < len(ctrlNames) {
		name = ctrlNames[typ]
	}
	rest := b[10:]
	switch typ {
	case 0:
		if len(rest) >= 3 {
			a.logf(v.TS, "%s ML %s client=%d service=%s flags=0x%02x", dir(v.ToDevice), name, client,
				svcName(binary.LittleEndian.Uint16(rest)), rest[2])
			return
		}
	case 1:
		if len(rest) >= 3 {
			svc := binary.LittleEndian.Uint16(rest)
			status := rest[2]
			if status == 0 && len(rest) >= 5 {
				h, rel := rest[3], rest[4] != 0
				a.finishPipe(h, v.TS)
				delete(a.streams, streamKey{h, true}) // a new registration restarts MLR sequence numbers
				delete(a.streams, streamKey{h, false})
				a.handles[h] = mlHandle{svc, rel}
				a.logf(v.TS, "%s ML %s client=%d service=%s status=0 handle=0x%02x reliable=%v extra=% x",
					dir(v.ToDevice), name, client, svcName(svc), h, rel, rest[5:])
				return
			}
			a.logf(v.TS, "%s ML %s client=%d service=%s status=%d rest=% x", dir(v.ToDevice), name, client,
				svcName(svc), status, rest[3:])
			return
		}
	case 2, 3:
		if len(rest) >= 3 {
			if typ == 3 {
				a.finishPipe(rest[2], v.TS)
			}
			a.logf(v.TS, "%s ML %s client=%d service=%s handle=0x%02x rest=% x", dir(v.ToDevice), name, client,
				svcName(binary.LittleEndian.Uint16(rest)), rest[2], rest[3:])
			return
		}
	}
	a.logf(v.TS, "%s ML %s client=%d rest=% x", dir(v.ToDevice), name, client, rest)
}

func (a *analysis) mlData(v btsnoop.ATTValue) {
	b := v.Value
	b0 := b[0]
	var h byte
	var payload []byte
	var hs mlHandle
	if b0&0x80 != 0 && len(b) >= 2 {
		hdr := multilink.ParseHeader(b[0], b[1])
		rh, sn := hdr.Handle, hdr.SN
		if hh, ok := a.handles[rh]; ok && hh.reliable {
			h, hs = rh, hh
			st := a.stream(h, v.ToDevice)
			if len(b) == 2 { // ack only
				return
			}
			if sn != st.mlr.expect {
				st.mlr.dups++
				return
			}
			st.mlr.expect = (st.mlr.expect + 1) % 64
			payload = b[2:]
		}
	}
	if payload == nil {
		hh, ok := a.handles[b0]
		if !ok {
			if verbose {
				a.logf(v.TS, "%s unknown ML handle 0x%02x len=%d", dir(v.ToDevice), b0, len(b))
			}
			return
		}
		h, hs, payload = b0, hh, b[1:]
	}
	st := a.stream(h, v.ToDevice)
	if hs.service == 1 {
		st.cobs = append(st.cobs, payload...)
		a.drainCOBS(v, st)
		return
	}
	if hs.service >= 0x2018 {
		a.pipeData(v, h, hs, payload)
		st.pipeLen += len(payload)
		if len(st.pipeHead) < 32 {
			st.pipeHead = append(st.pipeHead, head(payload, 32-len(st.pipeHead))...)
		}
	}
}

func head(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func (a *analysis) stream(h byte, toDev bool) *dirStream {
	k := streamKey{h, toDev}
	s := a.streams[k]
	if s == nil {
		s = &dirStream{}
		a.streams[k] = s
	}
	return s
}

func (a *analysis) drainCOBS(v btsnoop.ATTValue, st *dirStream) {
	for {
		// skip leading zeros
		i := 0
		for i < len(st.cobs) && st.cobs[i] == 0 {
			i++
		}
		st.cobs = st.cobs[i:]
		j := -1
		for k, c := range st.cobs {
			if c == 0 {
				j = k
				break
			}
		}
		if j < 0 {
			return
		}
		seg := st.cobs[:j]
		st.cobs = st.cobs[j:]
		frame, err := cobs.Decode(seg)
		if err != nil {
			a.logf(v.TS, "%s COBS decode error (%d bytes)", dir(v.ToDevice), len(seg))
			continue
		}
		st.frames++
		a.gfdi(v, frame)
	}
}

func (a *analysis) gfdi(v btsnoop.ATTValue, f []byte) {
	if len(f) < 6 {
		a.logf(v.TS, "%s GFDI short frame % x", dir(v.ToDevice), f)
		return
	}
	ln := int(binary.LittleEndian.Uint16(f))
	raw := binary.LittleEndian.Uint16(f[2:])
	id := raw
	compact := ""
	if raw&0x8000 != 0 {
		id = (raw & 0xFF) + 5000
		compact = fmt.Sprintf(" [compact seq=%d]", (raw>>8)&0x7F)
	}
	crcOK := ln == len(f) && gfdi.CRC16(0, f[:len(f)-2]) == binary.LittleEndian.Uint16(f[len(f)-2:])
	p := f[4 : len(f)-2]
	key := dir(v.ToDevice) + " " + gname(id)
	a.gfdiCount[key]++
	bad := ""
	if !crcOK {
		bad = fmt.Sprintf(" !! len/crc mismatch (len field %d, frame %d)", ln, len(f))
	}
	switch id {
	case 5004:
		a.ftdCount[v.ToDevice]++
		if len(p) >= 7 {
			a.ftdBytes[v.ToDevice] += len(p) - 7
		}
		if a.ftdCount[v.ToDevice] <= 2 || verbose {
			off := uint32(0)
			if len(p) >= 7 {
				off = binary.LittleEndian.Uint32(p[3:])
			}
			a.logf(v.TS, "%s FILE_TRANSFER_DATA%s offset=%d data=%d%s", dir(v.ToDevice), compact, off, len(p)-7, bad)
		}
		return
	case 5000:
		if len(p) >= 3 {
			orig := binary.LittleEndian.Uint16(p)
			st := p[2]
			if orig == 5004 && !verbose {
				return
			}
			extra := ""
			switch orig {
			case 5002:
				if len(p) >= 8 {
					extra = fmt.Sprintf(" downloadStatus=%d size=%d", p[3], binary.LittleEndian.Uint32(p[4:]))
				}
			case 5031:
				extra = describeFileTypes(p[3:])
			case 5101:
				extra = fmt.Sprintf(" data=% x", p[3:])
			case 5043, 5044:
				if len(p) >= 11 {
					extra = fmt.Sprintf(" reqId=%d offset=%d chunkStatus=%d code=%d", binary.LittleEndian.Uint16(p[3:]),
						binary.LittleEndian.Uint32(p[5:]), p[9], p[10])
				}
			default:
				if len(p) > 3 {
					extra = fmt.Sprintf(" data=% x", head(p[3:], 24))
				}
			}
			a.logf(v.TS, "%s RESPONSE%s to %s status=%d%s%s", dir(v.ToDevice), compact, gname(orig), st, extra, bad)
			return
		}
	case 5024:
		a.logf(v.TS, "%s DEVICE_INFORMATION%s %s%s", dir(v.ToDevice), compact, describeDevInfo(p), bad)
		return
	case 5050:
		a.logf(v.TS, "%s CONFIGURATION%s %s%s", dir(v.ToDevice), compact, describeCaps(p), bad)
		return
	case 5037:
		if len(p) >= 2 {
			n := int(p[1])
			var bits []int
			for i := 0; i < n && 2+i < len(p); i++ {
				for b := 0; b < 8; b++ {
					if p[2+i]&(1<<b) != 0 {
						bits = append(bits, i*8+b)
					}
				}
			}
			a.logf(v.TS, "%s SYNCHRONIZATION%s type=%d bits=%v%s", dir(v.ToDevice), compact, p[0], bits, bad)
			return
		}
	case 5002:
		if len(p) >= 13 {
			a.logf(v.TS, "%s DOWNLOAD_REQUEST%s index=%d offset=%d type=%d flags=% x%s", dir(v.ToDevice), compact,
				binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint32(p[2:]), p[6], p[13:], bad)
			return
		}
	case 5008:
		if len(p) >= 3 {
			a.logf(v.TS, "%s SET_FILE_FLAG%s index=%d flags=0x%02x%s", dir(v.ToDevice), compact,
				binary.LittleEndian.Uint16(p), p[2], bad)
			return
		}
	case 5030:
		if len(p) >= 1 {
			ev := fmt.Sprintf("event%d", p[0])
			if int(p[0]) < len(sysEvents) {
				ev = sysEvents[p[0]]
			}
			a.logf(v.TS, "%s SYSTEM_EVENT%s %s value=% x%s", dir(v.ToDevice), compact, ev, p[1:], bad)
			return
		}
	case 5043, 5044:
		if len(p) >= 14 {
			req := binary.LittleEndian.Uint16(p)
			off := binary.LittleEndian.Uint32(p[2:])
			total := binary.LittleEndian.Uint32(p[6:])
			chunk := binary.LittleEndian.Uint32(p[10:])
			data := p[14:]
			k := [2]uint64{boolU(v.ToDevice), uint64(req)}
			if off == 0 {
				a.protoBuf[k] = nil
			}
			a.protoBuf[k] = append(a.protoBuf[k], data...)
			if uint32(len(a.protoBuf[k])) >= total {
				desc := describeSmart(a.protoBuf[k])
				for _, part := range strings.Split(desc, " ") {
					a.smartSeen[dir(v.ToDevice)+" "+part]++
				}
				a.logf(v.TS, "%s %s%s reqId=%d total=%d %s%s", dir(v.ToDevice), gname(id), compact, req, total, desc, bad)
				if schema != nil {
					lines := schema.Render("garmin_vivomovehr.Smart", a.protoBuf[k])
					for _, l := range lines {
						fmt.Println("            | " + l)
					}
				}
				delete(a.protoBuf, k)
			} else if verbose {
				a.logf(v.TS, "%s %s%s reqId=%d chunk off=%d len=%d/%d", dir(v.ToDevice), gname(id), compact, req, off, chunk, total)
			}
			return
		}
	}
	a.logf(v.TS, "%s %s%s payload(%d)=% x%s", dir(v.ToDevice), gname(id), compact, len(p), head(p, 32), bad)
}

func boolU(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

func lpstr(b []byte, i int) (string, int) {
	if i >= len(b) {
		return "", i
	}
	n := int(b[i])
	if i+1+n > len(b) {
		return "", len(b)
	}
	return string(b[i+1 : i+1+n]), i + 1 + n
}

func describeDevInfo(p []byte) string {
	if len(p) < 12 {
		return fmt.Sprintf("% x", p)
	}
	pv := binary.LittleEndian.Uint16(p)
	prod := binary.LittleEndian.Uint16(p[2:])
	unit := binary.LittleEndian.Uint32(p[4:])
	sw := binary.LittleEndian.Uint16(p[8:])
	mps := binary.LittleEndian.Uint16(p[10:])
	s1, i := lpstr(p, 12)
	s2, i := lpstr(p, i)
	s3, i := lpstr(p, i)
	return fmt.Sprintf("protocol=%d product=%d unit=%d sw=%d.%02d maxPacket=%d names=%q/%q/%q tail=% x",
		pv, prod, unit, sw/100, sw%100, mps, s1, s2, s3, p[i:])
}

var capNames = map[int]string{3: "SYNC", 4: "DEVICE_INITIATES_SYNC", 5: "HOST_INITIATED_SYNC_REQUESTS",
	29: "EXPLICIT_ARCHIVE", 69: "EXPLORE_SYNC", 71: "CURRENT_TIME_REQUEST_SUPPORT", 76: "MULTI_LINK_SERVICE",
	77: "OAUTH_CREDENTIALS", 90: "SYNC_2", 92: "REALTIME_SETTINGS", 95: "FEATURE_CAPABILITIES_EXCHANGE"}

func describeCaps(p []byte) string {
	if len(p) < 1 {
		return ""
	}
	n := int(p[0])
	var bits []int
	for i := 0; i < n && 1+i < len(p); i++ {
		for b := 0; b < 8; b++ {
			if p[1+i]&(1<<b) != 0 {
				bits = append(bits, i*8+b)
			}
		}
	}
	has := map[int]bool{}
	for _, b := range bits {
		has[b] = true
	}
	var key []string
	for _, b := range []int{3, 4, 5, 29, 69, 71, 76, 77, 90, 92, 95} {
		key = append(key, fmt.Sprintf("%s=%v", capNames[b], has[b]))
	}
	return fmt.Sprintf("%d bytes, %d bits set; %s; all=%v", n, len(bits), strings.Join(key, " "), bits)
}

func describeFileTypes(p []byte) string {
	if len(p) < 1 {
		return ""
	}
	n := int(p[0])
	var items []string
	i := 1
	for k := 0; k < n && i+3 <= len(p); k++ {
		dt, st := p[i], p[i+1]
		var name string
		name, i = lpstr(p, i+2)
		items = append(items, fmt.Sprintf("%d/%d:%s", dt, st, name))
	}
	return fmt.Sprintf(" types(%d)=[%s]", n, strings.Join(items, " "))
}

// ---------------------------------------------------------------- main

// load reads a btsnoop HCI log, or a goarmin Multi-Link trace (.jsonl, pkg/garmin/mltrace) as
// if it were one, and prints what it holds.
func load(path string) ([]btsnoop.ATTValue, map[uint16]*btsnoop.ConnInfo, time.Time, error) {
	if strings.HasSuffix(path, ".jsonl") {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, time.Time{}, err
		}
		defer f.Close()
		tr, err := mltrace.Read(f)
		if err != nil {
			return nil, nil, time.Time{}, err
		}
		start := tr.Start
		if start.IsZero() && len(tr.Packets) > 0 {
			// An older trace without a start time: the file was last written at its last packet.
			if st, err := f.Stat(); err == nil {
				start = st.ModTime().Add(-tr.Packets[len(tr.Packets)-1].At)
			}
		}
		vals, conns := tr.ATT(start)
		var last time.Time
		if len(vals) > 0 {
			last = vals[len(vals)-1].TS
		}
		fmt.Printf("== %s\n   goarmin trace, %d Multi-Link packets, %s .. %s\n", path, len(vals),
			start.Format("2006-01-02 15:04:05"), last.Format("15:04:05"))
		return vals, conns, last, nil
	}
	recs, err := btsnoop.Read(path)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	conns := map[uint16]*btsnoop.ConnInfo{}
	vals, trunc := btsnoop.ExtractATT(recs, conns)
	var first, last time.Time
	if len(recs) > 0 {
		first, last = recs[0].TS, recs[len(recs)-1].TS
	}
	fmt.Printf("== %s\n   %d HCI records, %s .. %s, truncated records: %d\n", path, len(recs),
		first.Format("2006-01-02 15:04:05"), last.Format("15:04:05"), trunc)
	return vals, conns, last, nil
}

func isControl(v []byte) bool {
	return len(v) >= 10 && v[0] == 0 && v[1] <= 8 && binary.LittleEndian.Uint64(v[2:10]) < 16
}

func main() {
	flag.BoolVar(&verbose, "v", false, "verbose: print every file-data chunk, MLR detail and unknown handles")
	protoDir := flag.String("proto", protoschema.GadgetbridgeDir(), "directory of Garmin .proto files for field decoding (empty = off)")
	flag.StringVar(&pullDir, "out", "", "write files pulled over Multi-Link file pipes (inflated) into this directory")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: snoopdecode [-v] [-proto DIR] [-out DIR] <btsnoop_hci.log | goarmin trace .jsonl>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)
	if *protoDir != "" {
		s, err := protoschema.Load(*protoDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "schema:", err)
			os.Exit(1)
		}
		schema = s
	}
	vals, conns, last, err := load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Find the Garmin connection(s): any value on a 6a4e28xx characteristic, or a control-shaped write.
	garmin := map[uint16]map[uint16]bool{} // conn -> ATT handles carrying ML traffic
	for _, v := range vals {
		u := conns[v.ConnHandle].UUIDByHandle[v.Handle]
		if strings.HasPrefix(u, "6a4e28") || (v.ToDevice && isControl(v.Value)) {
			if garmin[v.ConnHandle] == nil {
				garmin[v.ConnHandle] = map[uint16]bool{}
			}
			garmin[v.ConnHandle][v.Handle] = true
		}
	}
	fmt.Println("== connections with ATT traffic")
	var hs []int
	for h := range conns {
		hs = append(hs, int(h))
	}
	sort.Ints(hs)
	for _, h := range hs {
		c := conns[uint16(h)]
		if c.Values == 0 {
			continue
		}
		mark := ""
		if garmin[uint16(h)] != nil {
			mark = "  <-- Garmin Multi-Link"
		}
		fmt.Printf("   conn 0x%03x peer=%s ATT values=%d truncated=%d known handles=%d%s\n", h, c.Peer, c.Values,
			c.Truncated, len(c.UUIDByHandle), mark)
	}
	for conn, mlHandles := range garmin {
		c := conns[conn]
		// The ML pair: TX = handles written to with control frames; RX = the notification handle
		// whose UUID is 6a4e28xx, or (no discovery) any notified handle sharing the conn.
		rx := map[uint16]bool{}
		for _, v := range vals {
			if v.ConnHandle != conn {
				continue
			}
			u := c.UUIDByHandle[v.Handle]
			if !v.ToDevice && (strings.HasPrefix(u, "6a4e281") || isControl(v.Value)) {
				rx[v.Handle] = true
			}
		}
		for h := range rx {
			mlHandles[h] = true
		}
		var hl []string
		for h := range mlHandles {
			hl = append(hl, fmt.Sprintf("0x%04x(%s)", h, c.UUIDByHandle[h]))
		}
		sort.Strings(hl)
		fmt.Printf("\n== Garmin conn 0x%03x peer=%s ML ATT handles: %s\n", conn, c.Peer, strings.Join(hl, " "))
		var gatt []string
		for h, u := range c.UUIDByHandle {
			if strings.Contains(u, "6a4e") || strings.HasPrefix(u, "svc ") {
				gatt = append(gatt, fmt.Sprintf("0x%04x=%s", h, u))
			}
		}
		sort.Strings(gatt)
		fmt.Printf("   GATT (services + Garmin chars): %s\n", strings.Join(gatt, " "))
		if c.Truncated > 0 {
			fmt.Printf("   WARNING: %d truncated ACL records on this connection; payload decoding may be incomplete\n", c.Truncated)
		}
		a := &analysis{handles: map[byte]mlHandle{}, streams: map[streamKey]*dirStream{}, clientIDs: map[uint64]bool{},
			gfdiCount: map[string]int{}, protoBuf: map[[2]uint64][]byte{}, smartSeen: map[string]int{},
			ftdCount: map[bool]int{}, ftdBytes: map[bool]int{}, pipes: map[byte]*pipeXfer{}}
		for _, v := range vals {
			if v.ConnHandle != conn || !mlHandles[v.Handle] || len(v.Value) == 0 {
				continue
			}
			if a.t0.IsZero() {
				a.t0 = v.TS
				fmt.Printf("   t0 = %s\n", v.TS.Format("2006-01-02 15:04:05.000"))
			}
			if v.Value[0] == 0 && isControl(v.Value) {
				a.control(v)
				continue
			}
			a.mlData(v)
		}
		for h := range a.pipes {
			a.finishPipe(h, last)
		}
		fmt.Println("\n== summary")
		fmt.Printf("   file pipe transfers finished: %d\n", a.pulled)
		var cl []string
		for id := range a.clientIDs {
			cl = append(cl, fmt.Sprint(id))
		}
		sort.Strings(cl)
		fmt.Printf("   ML client IDs seen: %s\n", strings.Join(cl, ","))
		var hkeys []int
		for h := range a.handles {
			hkeys = append(hkeys, int(h))
		}
		sort.Ints(hkeys)
		for _, h := range hkeys {
			hh := a.handles[byte(h)]
			fmt.Printf("   handle 0x%02x = %s reliable=%v (last registration)\n", h, svcName(hh.service), hh.reliable)
		}
		var skeys []streamKey
		for k := range a.streams {
			skeys = append(skeys, k)
		}
		sort.Slice(skeys, func(i, j int) bool {
			if skeys[i].handle != skeys[j].handle {
				return skeys[i].handle < skeys[j].handle
			}
			return !skeys[i].toDev && skeys[j].toDev
		})
		for _, k := range skeys {
			s := a.streams[k]
			hh := a.handles[k.handle]
			fmt.Printf("   stream handle 0x%02x %s %s: gfdiFrames=%d mlrDupsDropped=%d pipeBytes=%d pipeHead=% x\n",
				k.handle, dir(k.toDev), svcName(hh.service), s.frames, s.mlr.dups, s.pipeLen, s.pipeHead)
		}
		fmt.Printf("   FILE_TRANSFER_DATA: W→H %d chunks / %d bytes, H→W %d chunks\n", a.ftdCount[false], a.ftdBytes[false], a.ftdCount[true])
		var keys []string
		for k := range a.gfdiCount {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println("   GFDI message counts:")
		for _, k := range keys {
			fmt.Printf("     %-45s %d\n", k, a.gfdiCount[k])
		}
		keys = keys[:0]
		for k := range a.smartSeen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println("   Smart protobuf services/messages:")
		for _, k := range keys {
			fmt.Printf("     %-60s %d\n", k, a.smartSeen[k])
		}
	}
	if len(garmin) == 0 {
		fmt.Println("\nNo Garmin Multi-Link traffic found.")
	}
}

// pipeData follows one Multi-Link file-transfer pipe: a 6-byte configure from the host
// (00 | dir | token u16 | 00 00), a 3-byte reply from the watch, then zlib data.
func (a *analysis) pipeData(v btsnoop.ATTValue, h byte, hs mlHandle, payload []byte) {
	p := a.pipes[h]
	if v.ToDevice {
		isCfg := len(payload) >= 6 && payload[0] == 0 && payload[1] <= 1 && payload[4] == 0 && payload[5] == 0 &&
			(len(payload) == 6 || (payload[1] == 1 && len(payload) >= 8 && payload[6]&0x0f == 8))
		if isCfg && (p == nil || len(p.tx) > 0 || len(p.rx) > 0 || p.gotReply) {
			a.finishPipe(h, v.TS)
			p = nil
		}
		if isCfg && p == nil {
			p = &pipeXfer{svc: hs.service, dir: payload[1], token: binary.LittleEndian.Uint16(payload[2:]), ts: v.TS}
			a.pipes[h] = p
			a.logf(v.TS, "H→W %s pipe configure dir=%s token=0x%04x", svcName(hs.service), map[byte]string{0: "pull", 1: "push"}[p.dir], p.token)
			payload = payload[6:]
		}
		if p != nil {
			p.tx = append(p.tx, payload...)
		}
		return
	}
	if p == nil {
		return
	}
	if !p.gotReply {
		if len(payload) >= 3 {
			a.logf(v.TS, "W→H %s pipe reply % x", svcName(hs.service), payload[:3])
			payload = payload[3:]
		}
		p.gotReply = true
	}
	p.rx = append(p.rx, payload...)
}

func (a *analysis) finishPipe(h byte, ts time.Time) {
	p := a.pipes[h]
	if p == nil {
		return
	}
	delete(a.pipes, h)
	data := p.rx
	what := "pull"
	if p.dir == 1 {
		data, what = p.tx, "push"
	}
	if len(data) == 0 {
		a.logf(ts, "PIPE %s token=0x%04x %s: no data", svcName(p.svc), p.token, what)
		return
	}
	a.pulled++
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		a.logf(ts, "PIPE %s token=0x%04x %s: %d bytes, not zlib (%v), head % x", svcName(p.svc), p.token, what, len(data), err, head(data, 16))
		return
	}
	plain, err := io.ReadAll(zr)
	note := ""
	if err != nil {
		note = fmt.Sprintf(" (inflate stopped: %v)", err)
	}
	fit := len(plain) >= 12 && string(plain[8:12]) == ".FIT"
	a.logf(ts, "PIPE %s token=0x%04x %s: zlib %d -> %d bytes, FIT=%v, head % x%s", svcName(p.svc), p.token, what, len(data), len(plain), fit, head(plain, 14), note)
	if pullDir != "" && p.dir == 0 && len(plain) > 0 {
		ext := ".bin"
		if fit {
			ext = ".fit"
		}
		_ = os.MkdirAll(pullDir, 0o755)
		_ = os.WriteFile(filepath.Join(pullDir, fmt.Sprintf("token_%04x%s", p.token, ext)), plain, 0o644)
	}
}
