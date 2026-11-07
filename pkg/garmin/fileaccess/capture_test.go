// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/karamble/goarmin/pkg/btsnoop"
	"github.com/karamble/goarmin/pkg/garmin/capture"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// TestCaptureReplay decodes a real Garmin Connect sync (an Android HCI snoop log, kept outside
// the repo) with goarmin's stack. It checks four things:
//   - every frame, protobuf message and transfer decodes;
//   - every listed item's type name resolves;
//   - each pulled file inflates;
//   - the host messages goarmin sends match Garmin Connect's byte for byte, identifiers aside.
//
// Run it with GOARMIN_CAPTURE=~/goarmin-captures/<dir>/btsnoop_hci.log.filtered.last.
func TestCaptureReplay(t *testing.T) {
	path := os.Getenv("GOARMIN_CAPTURE")
	if path == "" {
		t.Skip("set GOARMIN_CAPTURE to a btsnoop log of a Garmin Connect sync")
	}
	recs, err := btsnoop.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	conns := map[uint16]*btsnoop.ConnInfo{}
	vals, _ := btsnoop.ExtractATT(recs, conns)
	cs := capture.GarminConnections(vals, conns)
	if len(cs) == 0 {
		t.Fatal("no Multi-Link traffic in the capture")
	}
	res := capture.Decode(vals, conns, cs[0])
	t.Log(res)
	if res.FrameErrors != 0 {
		t.Errorf("%d frames failed to decode", res.FrameErrors)
	}
	msgs, errs := capture.Messages(res.Frames)
	if errs != 0 {
		t.Errorf("%d protobuf chunks did not reassemble", errs)
	}

	// Host handshake messages.
	for _, f := range res.Frames {
		if !f.ToDevice {
			continue
		}
		switch f.ID {
		case gfdi.Configuration:
			if !bytes.Equal(f.Payload, gfdi.GarminConnectHostCapabilities().Payload()) {
				t.Errorf("host CONFIGURATION % X differs from ours", f.Payload)
			}
		case gfdi.DeviceSettings:
			if !bytes.Equal(f.Payload, []byte{3, 6, 1, 1, 7, 1, 1, 8, 1, 0}) {
				t.Errorf("DEVICE_SETTINGS % X differs from ours", f.Payload)
			}
		}
	}

	kinds, priorities := map[string]int{}, map[string]int{}
	keys := map[uint32]string{}
	var compared, items, pullsOK int
	same := func(what string, got smart.FileAccess, want []byte) {
		t.Helper()
		compared++
		if g := got.Smart(); !bytes.Equal(g, want) {
			t.Errorf("%s: ours\n % X\nGarmin Connect\n % X", what, g, want)
		}
	}
	for _, m := range msgs {
		env, err := smart.ParseEnvelope(m.Data)
		if err != nil {
			t.Errorf("%v request %d: %v", m.Kind, m.RequestID, err)
			continue
		}
		if env.Service != smart.ServiceFileAccess {
			continue
		}
		fa, err := env.FileAccess()
		if err != nil {
			t.Errorf("FileAccess %v request %d: %v", m.Kind, m.RequestID, err)
			continue
		}
		dir := "W→H "
		if m.ToDevice {
			dir = "H→W "
		}
		kinds[dir+fa.Kind()]++
		switch {
		case fa.ItemListRequest != nil:
			r := fa.ItemListRequest
			switch {
			case r.SessionID != nil && r.TransactionID == nil && len(r.GetFlags) == 0:
				same("listing continuation", fileaccess.ContinueListRequest(*r.SessionID), m.Data)
			case r.SessionID == nil && len(r.IncludedDataTypes) == 0 && r.MaxItemCount == nil && len(r.RequestedMetadata) == 0:
				same("listing", fileaccess.ListRequest(r.TransactionID), m.Data)
			default:
				kinds["(not compared) listing with data types"]++
			}
		case fa.PullItemRequest != nil:
			r := fa.PullItemRequest
			if r.Item == nil || r.Item.UID == nil || r.Priority == nil || r.RequestedCompressionWindow == nil {
				t.Errorf("unexpected pull shape: %+v", r)
				continue
			}
			priorities["pull "+fmt.Sprint(*r.Priority)]++
			same("pull", fileaccess.PullRequest(*r.Item.UID, *r.Priority, *r.RequestedCompressionWindow), m.Data)
		case fa.ModifyFlagsRequest != nil && fa.ModifyFlagsRequest.UID != nil:
			same("archive", fileaccess.ArchiveRequest(*fa.ModifyFlagsRequest.UID), m.Data)
		case fa.TransferStatusResponse != nil:
			next := fa.TransferStatusResponse.NextTransferPriority
			if next == nil {
				priorities["next none"]++
			} else {
				priorities["next "+fmt.Sprint(*next)]++
			}
			same("transfer status reply", fileaccess.TransferStatusReply(next), m.Data)
		case fa.CancelTransferRequest != nil && fa.CancelTransferRequest.TransferHandle != nil:
			same("cancel", fileaccess.CancelRequest(*fa.CancelTransferRequest.TransferHandle), m.Data)
		case fa.ItemListResponse != nil:
			for _, ref := range fa.ItemListResponse.Items {
				items++
				dt := ref.DataType
				if dt == nil || dt.GDXMLStringKey == nil {
					continue
				}
				if dt.GDXMLString != nil {
					keys[*dt.GDXMLStringKey] = *dt.GDXMLString
				} else if keys[*dt.GDXMLStringKey] == "" {
					t.Errorf("item %v uses key %d before its name was given", ref.UID, *dt.GDXMLStringKey)
				}
			}
		case fa.PullItemResponse != nil:
			if r := fa.PullItemResponse.Result; r == nil || *r == smart.ItemAccessSuccess {
				pullsOK++
			}
		}
	}
	pulls := 0
	for _, tr := range res.Transfers {
		if tr.Push {
			continue // pushes are out of scope; the last ones were cut off by the log rotation
		}
		data, err := tr.Inflate()
		if err != nil {
			t.Errorf("transfer token %d on %s (push %v): %v", tr.Token, tr.Service, tr.Push, err)
			continue
		}
		pulls++
		if len(data) == 0 {
			t.Errorf("transfer token %d is empty", tr.Token)
		}
	}
	if pulls != pullsOK {
		t.Errorf("%d pulls inflated, %d PullItemDataResponses said SUCCESS", pulls, pullsOK)
	}
	if compared == 0 {
		t.Error("no host FileAccess message to compare")
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t.Logf("%4d × %s", kinds[k], k)
	}
	t.Logf("Garmin Connect's priorities: %v", priorities)
	t.Logf("%d host messages identical to Garmin Connect's, %d items listed with %d type names, %d pulls inflated",
		compared, items, len(keys), pulls)
}
