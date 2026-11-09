// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import "github.com/karamble/goarmin/pkg/garmin/smart"

// The host requests, shaped exactly as Garmin Connect sends them to the Fenix 8
// (docs/protocol/05 §8). The capture replay test compares them byte for byte.

// ListRequest starts a listing of everything not flagged as synced, incremental from cursor
// if it is not nil.
func ListRequest(cursor *uint32) smart.FileAccess {
	req := &smart.ItemListRequest{ExcludeAllFlags: []smart.UUID{smart.SyncedFlag}, GetFlags: []smart.UUID{smart.SyncedFlag}}
	if cursor != nil {
		req.TransactionID = smart.U32(*cursor)
	}
	return smart.FileAccess{ItemListRequest: req}
}

// ListAllRequest lists every item, synced or not, incremental from cursor if it is not nil.
// Garmin Connect never sends it; it finds files another host has already archived but the
// watch still keeps (activities, for example).
func ListAllRequest(cursor *uint32) smart.FileAccess {
	req := &smart.ItemListRequest{GetFlags: []smart.UUID{smart.SyncedFlag}}
	if cursor != nil {
		req.TransactionID = smart.U32(*cursor)
	}
	return smart.FileAccess{ItemListRequest: req}
}

// InventoryRequest lists every item of the given GDXML types ("FIT_TYPE_5"), synced or not,
// from the start: what the watch keeps of those types now. Garmin Connect filters a listing
// by data type the same way (by format, for its GDXML device file); a watch that ignores the
// filter lists everything, which Engine.Inventory filters itself.
func InventoryRequest(types []string) smart.FileAccess {
	req := &smart.ItemListRequest{GetFlags: []smart.UUID{smart.SyncedFlag}}
	for _, t := range types {
		req.IncludedDataTypes = append(req.IncludedDataTypes, smart.DataType{Format: smart.Ptr(smart.FormatGDXMLDataType), GDXMLString: smart.S(t)})
	}
	return smart.FileAccess{ItemListRequest: req}
}

// ContinueListRequest asks for the next page of a listing.
func ContinueListRequest(session uint32) smart.FileAccess {
	return smart.FileAccess{ItemListRequest: &smart.ItemListRequest{SessionID: smart.U32(session)}}
}

// PullRequest asks for an item over a Multi-Link pipe, from offset 0. Only the uid identifies
// the item, as with Garmin Connect.
func PullRequest(uid smart.UUID, priority, window uint32) smart.FileAccess {
	return smart.FileAccess{PullItemRequest: &smart.PullItemDataRequest{
		Item:                       &smart.ItemReference{UID: smart.Ptr(uid)},
		Priority:                   smart.U32(priority),
		SupportedTransports:        []smart.TransportProtocol{smart.TransportMultilinkPipe},
		Offset:                     smart.U64(0),
		RequestedCompressionWindow: smart.U32(window),
	}}
}

// PushRequest offers the watch a file of type typeName (a GDXML name such as "FIT_TYPE_5")
// under the host's id uid, as Garmin Connect does (docs/protocol/08): priority 34, a Multi-Link
// pipe, window 15 asked for. size is the clear (uncompressed) size.
func PushRequest(uid smart.UUID, typeName string, size uint64, window uint32) smart.FileAccess {
	return smart.FileAccess{PushItemRequest: &smart.PushItemDataRequest{
		Item: &smart.ItemReference{UID: smart.Ptr(uid),
			DataType: &smart.DataType{Format: smart.Ptr(smart.FormatGDXMLDataType), GDXMLString: smart.S(typeName)},
			DataSize: smart.U64(size)},
		Priority:                   smart.U32(PushPriority),
		SupportedTransports:        []smart.TransportProtocol{smart.TransportMultilinkPipe},
		RequestedCompressionWindow: smart.U32(window),
	}}
}

// PushPriority is the priority Garmin Connect gives its pushes.
const PushPriority = 34

// ArchiveRequest flags an item as synced.
func ArchiveRequest(uid smart.UUID) smart.FileAccess {
	return smart.FileAccess{ModifyFlagsRequest: &smart.ModifyFlagsRequest{UID: smart.Ptr(uid), SetFlags: []smart.UUID{smart.SyncedFlag}}}
}

// DeleteRequest asks the watch to delete an item it offered for pulling. No other client is
// known to send it (docs/protocol/04 §3.7).
func DeleteRequest(uid smart.UUID) smart.FileAccess {
	return DeleteRequestIn(uid, smart.DirectionPull)
}

// DeleteRequestIn asks the watch to delete an item, naming the direction it came in: pull for
// an item the watch offered, push for one the host pushed.
func DeleteRequestIn(uid smart.UUID, dir smart.TransferDirection) smart.FileAccess {
	return smart.FileAccess{DeleteItemRequest: &smart.DeleteItemRequest{UID: smart.Ptr(uid), Direction: smart.Ptr(dir)}}
}

// TransferStatusReply answers the watch's TransferStatusRequest with the priority of the
// host's next transfer, or with an empty message when nothing more is queued. Garmin Connect
// sends 20, 24, 34 or 44 here, and nothing after its last transfer (capture replay).
func TransferStatusReply(next *uint32) smart.FileAccess {
	return smart.FileAccess{TransferStatusResponse: &smart.TransferStatusResponse{NextTransferPriority: next}}
}

// CancelRequest stops a transfer.
func CancelRequest(handle uint32) smart.FileAccess {
	return smart.FileAccess{CancelTransferRequest: &smart.CancelTransferRequest{TransferHandle: smart.U32(handle)}}
}
