// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"strconv"
	"strings"
)

// FileAccess is GDI.Proto.FileAccess.Service, Smart field 43 (ServiceFileAccess). Every
// message sets exactly one field. The proto declares them all optional, not as a oneof, so
// ParseFileAccess accepts several and Marshal emits whatever is set. Field 8 is not defined.
type FileAccess struct {
	PullItemRequest                  *PullItemDataRequest              // 1, host to watch
	PullItemResponse                 *PullItemDataResponse             // 2, watch to host
	PushItemRequest                  *PushItemDataRequest              // 3, host to watch
	PushItemResponse                 *PushItemDataResponse             // 4, watch to host
	TransferStatusRequest            *TransferStatusRequest            // 5, watch to host
	PriorityUpdateRequest            *PriorityUpdateRequest            // 6, host to watch
	PriorityUpdateResponse           *PriorityUpdateResponse           // 7, watch to host
	ItemListRequest                  *ItemListRequest                  // 9, host to watch
	ItemListResponse                 *ItemListResponse                 // 10, watch to host
	ItemListCancel                   *ItemListCancelNotification       // 11, watch to host
	ItemAdded                        *ItemAddedNotification            // 12, watch to host
	DeleteItemRequest                *DeleteItemRequest                // 13, host to watch
	DeleteItemResponse               *DeleteItemResponse               // 14, watch to host
	ModifyFlagsRequest               *ModifyFlagsRequest               // 15, host to watch
	ModifyFlagsResponse              *ModifyFlagsResponse              // 16, watch to host
	ItemUpdated                      *ItemUpdatedNotification          // 17, watch to host
	CancelTransferRequest            *CancelTransferRequest            // 18, host to watch
	CancelTransferResponse           *CancelTransferResponse           // 19, watch to host
	ResourceUpdate                   *ResourceUpdateNotification       // 20, watch to host
	TransferStatusResponse           *TransferStatusResponse           // 21, host to watch
	SyncButton                       *SyncButtonNotification           // 22, watch to host
	SoftwareUpdatePartNumberRequest  *SoftwareUpdatePartNumberRequest  // 23
	SoftwareUpdatePartNumberResponse *SoftwareUpdatePartNumberResponse // 24
	GetItemChecksumRequest           *GetItemChecksumRequest           // 25, host to watch
	GetItemChecksumResponse          *GetItemChecksumResponse          // 26, watch to host

	unknown []byte
}

// ParseFileAccess decodes a FileAccess service message (the payload of a ServiceFileAccess
// Envelope).
func ParseFileAccess(b []byte) (FileAccess, error) { return parse[FileAccess](b) }

// Marshal encodes the service message, without the Smart envelope.
func (f FileAccess) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, f.PullItemRequest)
	b = encMsg(b, 2, f.PullItemResponse)
	b = encMsg(b, 3, f.PushItemRequest)
	b = encMsg(b, 4, f.PushItemResponse)
	b = encMsg(b, 5, f.TransferStatusRequest)
	b = encMsg(b, 6, f.PriorityUpdateRequest)
	b = encMsg(b, 7, f.PriorityUpdateResponse)
	b = encMsg(b, 9, f.ItemListRequest)
	b = encMsg(b, 10, f.ItemListResponse)
	b = encMsg(b, 11, f.ItemListCancel)
	b = encMsg(b, 12, f.ItemAdded)
	b = encMsg(b, 13, f.DeleteItemRequest)
	b = encMsg(b, 14, f.DeleteItemResponse)
	b = encMsg(b, 15, f.ModifyFlagsRequest)
	b = encMsg(b, 16, f.ModifyFlagsResponse)
	b = encMsg(b, 17, f.ItemUpdated)
	b = encMsg(b, 18, f.CancelTransferRequest)
	b = encMsg(b, 19, f.CancelTransferResponse)
	b = encMsg(b, 20, f.ResourceUpdate)
	b = encMsg(b, 21, f.TransferStatusResponse)
	b = encMsg(b, 22, f.SyncButton)
	b = encMsg(b, 23, f.SoftwareUpdatePartNumberRequest)
	b = encMsg(b, 24, f.SoftwareUpdatePartNumberResponse)
	b = encMsg(b, 25, f.GetItemChecksumRequest)
	b = encMsg(b, 26, f.GetItemChecksumResponse)
	return append(b, f.unknown...)
}

// Smart encodes the message inside its Smart envelope, ready for a PROTOBUF_REQUEST or
// PROTOBUF_RESPONSE payload.
func (f FileAccess) Smart() []byte {
	return Envelope{Service: ServiceFileAccess, Payload: f.Marshal()}.Marshal()
}

// Kind names the set field for logs, e.g. "ItemListResponse". Several set fields are joined
// with "+"; a message with only unknown fields gives "Unknown(23)" (field numbers), and an
// empty one "Empty".
func (f FileAccess) Kind() string {
	var k []string
	add := func(set bool, name string) {
		if set {
			k = append(k, name)
		}
	}
	add(f.PullItemRequest != nil, "PullItemRequest")
	add(f.PullItemResponse != nil, "PullItemResponse")
	add(f.PushItemRequest != nil, "PushItemRequest")
	add(f.PushItemResponse != nil, "PushItemResponse")
	add(f.TransferStatusRequest != nil, "TransferStatusRequest")
	add(f.PriorityUpdateRequest != nil, "PriorityUpdateRequest")
	add(f.PriorityUpdateResponse != nil, "PriorityUpdateResponse")
	add(f.ItemListRequest != nil, "ItemListRequest")
	add(f.ItemListResponse != nil, "ItemListResponse")
	add(f.ItemListCancel != nil, "ItemListCancel")
	add(f.ItemAdded != nil, "ItemAdded")
	add(f.DeleteItemRequest != nil, "DeleteItemRequest")
	add(f.DeleteItemResponse != nil, "DeleteItemResponse")
	add(f.ModifyFlagsRequest != nil, "ModifyFlagsRequest")
	add(f.ModifyFlagsResponse != nil, "ModifyFlagsResponse")
	add(f.ItemUpdated != nil, "ItemUpdated")
	add(f.CancelTransferRequest != nil, "CancelTransferRequest")
	add(f.CancelTransferResponse != nil, "CancelTransferResponse")
	add(f.ResourceUpdate != nil, "ResourceUpdate")
	add(f.TransferStatusResponse != nil, "TransferStatusResponse")
	add(f.SyncButton != nil, "SyncButton")
	add(f.SoftwareUpdatePartNumberRequest != nil, "SoftwareUpdatePartNumberRequest")
	add(f.SoftwareUpdatePartNumberResponse != nil, "SoftwareUpdatePartNumberResponse")
	add(f.GetItemChecksumRequest != nil, "GetItemChecksumRequest")
	add(f.GetItemChecksumResponse != nil, "GetItemChecksumResponse")
	switch {
	case len(k) > 0:
		return strings.Join(k, "+")
	case len(f.unknown) > 0:
		var nums []string
		for _, n := range fieldNumbers(f.unknown) {
			nums = append(nums, strconv.Itoa(n))
		}
		return "Unknown(" + strings.Join(nums, ",") + ")"
	}
	return "Empty"
}

func (f *FileAccess) unmarshal(b []byte) error {
	return walk("FileAccess", b, &f.unknown, func(fd *field) (bool, error) {
		switch fd.num {
		case 1:
			return true, decMsg(&f.PullItemRequest, fd)
		case 2:
			return true, decMsg(&f.PullItemResponse, fd)
		case 3:
			return true, decMsg(&f.PushItemRequest, fd)
		case 4:
			return true, decMsg(&f.PushItemResponse, fd)
		case 5:
			return true, decMsg(&f.TransferStatusRequest, fd)
		case 6:
			return true, decMsg(&f.PriorityUpdateRequest, fd)
		case 7:
			return true, decMsg(&f.PriorityUpdateResponse, fd)
		case 9:
			return true, decMsg(&f.ItemListRequest, fd)
		case 10:
			return true, decMsg(&f.ItemListResponse, fd)
		case 11:
			return true, decMsg(&f.ItemListCancel, fd)
		case 12:
			return true, decMsg(&f.ItemAdded, fd)
		case 13:
			return true, decMsg(&f.DeleteItemRequest, fd)
		case 14:
			return true, decMsg(&f.DeleteItemResponse, fd)
		case 15:
			return true, decMsg(&f.ModifyFlagsRequest, fd)
		case 16:
			return true, decMsg(&f.ModifyFlagsResponse, fd)
		case 17:
			return true, decMsg(&f.ItemUpdated, fd)
		case 18:
			return true, decMsg(&f.CancelTransferRequest, fd)
		case 19:
			return true, decMsg(&f.CancelTransferResponse, fd)
		case 20:
			return true, decMsg(&f.ResourceUpdate, fd)
		case 21:
			return true, decMsg(&f.TransferStatusResponse, fd)
		case 22:
			return true, decMsg(&f.SyncButton, fd)
		case 23:
			return true, decMsg(&f.SoftwareUpdatePartNumberRequest, fd)
		case 24:
			return true, decMsg(&f.SoftwareUpdatePartNumberResponse, fd)
		case 25:
			return true, decMsg(&f.GetItemChecksumRequest, fd)
		case 26:
			return true, decMsg(&f.GetItemChecksumResponse, fd)
		}
		return false, nil
	})
}
