// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

// ModifyFlagsRequest is GDI.Proto.FileAccess.ModifyFlagsRequest (FileAccess field 15, host to
// watch). Archiving an item is {UID, SetFlags: [SyncedFlag]}; send it only after the item is
// stored durably, because the watch may delete health files once they are flagged.
type ModifyFlagsRequest struct {
	UID        *UUID  // 1
	SetFlags   []UUID // 2
	ClearFlags []UUID // 3

	unknown []byte
}

// ParseModifyFlagsRequest decodes a ModifyFlagsRequest message.
func ParseModifyFlagsRequest(b []byte) (ModifyFlagsRequest, error) {
	return parse[ModifyFlagsRequest](b)
}

// Marshal encodes the message.
func (m ModifyFlagsRequest) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.UID)
	b = encMsgs(b, 2, m.SetFlags)
	b = encMsgs(b, 3, m.ClearFlags)
	return append(b, m.unknown...)
}

func (m *ModifyFlagsRequest) unmarshal(b []byte) error {
	return walk("ModifyFlagsRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decMsgs(&m.SetFlags, f)
		case 3:
			return true, decMsgs(&m.ClearFlags, f)
		}
		return false, nil
	})
}

// ModifyFlagsResponse is GDI.Proto.FileAccess.ModifyFlagsResponse (FileAccess field 16, watch
// to host).
type ModifyFlagsResponse struct {
	Status      *ModifyFlagsStatus // 1
	ErrorSource *uint32            // 2
	ErrorCode   *uint32            // 3

	unknown []byte
}

// ParseModifyFlagsResponse decodes a ModifyFlagsResponse message.
func ParseModifyFlagsResponse(b []byte) (ModifyFlagsResponse, error) {
	return parse[ModifyFlagsResponse](b)
}

// Marshal encodes the message.
func (m ModifyFlagsResponse) Marshal() []byte {
	b := encEnum(nil, 1, m.Status)
	b = encU32(b, 2, m.ErrorSource)
	b = encU32(b, 3, m.ErrorCode)
	return append(b, m.unknown...)
}

func (m *ModifyFlagsResponse) unmarshal(b []byte) error {
	return walk("ModifyFlagsResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Status, f)
		case 2:
			return true, decU32(&m.ErrorSource, f)
		case 3:
			return true, decU32(&m.ErrorCode, f)
		}
		return false, nil
	})
}

// ItemUpdatedNotification is GDI.Proto.FileAccess.ItemUpdatedNotification (FileAccess field
// 17, watch to host): an item's flags changed, typically after a ModifyFlagsRequest.
type ItemUpdatedNotification struct {
	UID               *UUID  // 1
	ChangedFlagsSet   []UUID // 2
	ChangedFlagsClear []UUID // 3

	unknown []byte
}

// ParseItemUpdatedNotification decodes an ItemUpdatedNotification message.
func ParseItemUpdatedNotification(b []byte) (ItemUpdatedNotification, error) {
	return parse[ItemUpdatedNotification](b)
}

// Marshal encodes the message.
func (m ItemUpdatedNotification) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.UID)
	b = encMsgs(b, 2, m.ChangedFlagsSet)
	b = encMsgs(b, 3, m.ChangedFlagsClear)
	return append(b, m.unknown...)
}

func (m *ItemUpdatedNotification) unmarshal(b []byte) error {
	return walk("ItemUpdatedNotification", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decMsgs(&m.ChangedFlagsSet, f)
		case 3:
			return true, decMsgs(&m.ChangedFlagsClear, f)
		}
		return false, nil
	})
}

// DeleteItemRequest is GDI.Proto.FileAccess.DeleteItemRequest (FileAccess field 13, host to
// watch).
type DeleteItemRequest struct {
	UID       *UUID              // 1
	Direction *TransferDirection // 2

	unknown []byte
}

// ParseDeleteItemRequest decodes a DeleteItemRequest message.
func ParseDeleteItemRequest(b []byte) (DeleteItemRequest, error) {
	return parse[DeleteItemRequest](b)
}

// Marshal encodes the message.
func (m DeleteItemRequest) Marshal() []byte {
	b := encMsg(nil, 1, m.UID)
	b = encEnum(b, 2, m.Direction)
	return append(b, m.unknown...)
}

func (m *DeleteItemRequest) unmarshal(b []byte) error {
	return walk("DeleteItemRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decEnum(&m.Direction, f)
		}
		return false, nil
	})
}

// DeleteItemResponse is GDI.Proto.FileAccess.DeleteItemResponse (FileAccess field 14, watch
// to host).
type DeleteItemResponse struct {
	Result      *DeleteItemResult // 1
	ErrorCode   *uint32           // 2
	ErrorSource *uint32           // 3

	unknown []byte
}

// ParseDeleteItemResponse decodes a DeleteItemResponse message.
func ParseDeleteItemResponse(b []byte) (DeleteItemResponse, error) {
	return parse[DeleteItemResponse](b)
}

// Marshal encodes the message.
func (m DeleteItemResponse) Marshal() []byte {
	b := encEnum(nil, 1, m.Result)
	b = encU32(b, 2, m.ErrorCode)
	b = encU32(b, 3, m.ErrorSource)
	return append(b, m.unknown...)
}

func (m *DeleteItemResponse) unmarshal(b []byte) error {
	return walk("DeleteItemResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Result, f)
		case 2:
			return true, decU32(&m.ErrorCode, f)
		case 3:
			return true, decU32(&m.ErrorSource, f)
		}
		return false, nil
	})
}

// GetItemChecksumRequest is GDI.Proto.FileAccess.GetItemChecksumRequest (FileAccess field 25,
// host to watch).
type GetItemChecksumRequest struct {
	UID *UUID // 1

	unknown []byte
}

// ParseGetItemChecksumRequest decodes a GetItemChecksumRequest message.
func ParseGetItemChecksumRequest(b []byte) (GetItemChecksumRequest, error) {
	return parse[GetItemChecksumRequest](b)
}

// Marshal encodes the message.
func (m GetItemChecksumRequest) Marshal() []byte {
	return append(encMsg(nil, 1, m.UID), m.unknown...)
}

func (m *GetItemChecksumRequest) unmarshal(b []byte) error {
	return walk("GetItemChecksumRequest", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decMsg(&m.UID, f)
		}
		return false, nil
	})
}

// GetItemChecksumResponse is GDI.Proto.FileAccess.GetItemChecksumResponse (FileAccess field
// 26, watch to host).
type GetItemChecksumResponse struct {
	UID                  *UUID           // 1
	Result               *ChecksumResult // 2
	ChecksumTruncatedMD5 *uint64         // 3: fixed64, LE uint64 of MD5(clear file)[0:8]

	unknown []byte
}

// ParseGetItemChecksumResponse decodes a GetItemChecksumResponse message.
func ParseGetItemChecksumResponse(b []byte) (GetItemChecksumResponse, error) {
	return parse[GetItemChecksumResponse](b)
}

// Marshal encodes the message.
func (m GetItemChecksumResponse) Marshal() []byte {
	b := encMsg(nil, 1, m.UID)
	b = encEnum(b, 2, m.Result)
	b = encFixed64(b, 3, m.ChecksumTruncatedMD5)
	return append(b, m.unknown...)
}

func (m *GetItemChecksumResponse) unmarshal(b []byte) error {
	return walk("GetItemChecksumResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decEnum(&m.Result, f)
		case 3:
			return true, decFixed64(&m.ChecksumTruncatedMD5, f)
		}
		return false, nil
	})
}

// SoftwareUpdatePartNumberRequest is GDI.Proto.FileAccess.SoftwareUpdatePartNumberRequest
// (FileAccess field 23).
type SoftwareUpdatePartNumberRequest struct {
	Requestor *Requestor // 1

	unknown []byte
}

// ParseSoftwareUpdatePartNumberRequest decodes a SoftwareUpdatePartNumberRequest message.
func ParseSoftwareUpdatePartNumberRequest(b []byte) (SoftwareUpdatePartNumberRequest, error) {
	return parse[SoftwareUpdatePartNumberRequest](b)
}

// Marshal encodes the message.
func (m SoftwareUpdatePartNumberRequest) Marshal() []byte {
	return append(encEnum(nil, 1, m.Requestor), m.unknown...)
}

func (m *SoftwareUpdatePartNumberRequest) unmarshal(b []byte) error {
	return walk("SoftwareUpdatePartNumberRequest", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decEnum(&m.Requestor, f)
		}
		return false, nil
	})
}

// SoftwareUpdatePartNumberResponse is GDI.Proto.FileAccess.SoftwareUpdatePartNumberResponse
// (FileAccess field 24).
type SoftwareUpdatePartNumberResponse struct {
	PartNumbers []string // 1: part_number

	unknown []byte
}

// ParseSoftwareUpdatePartNumberResponse decodes a SoftwareUpdatePartNumberResponse message.
func ParseSoftwareUpdatePartNumberResponse(b []byte) (SoftwareUpdatePartNumberResponse, error) {
	return parse[SoftwareUpdatePartNumberResponse](b)
}

// Marshal encodes the message.
func (m SoftwareUpdatePartNumberResponse) Marshal() []byte {
	return append(encStrings(nil, 1, m.PartNumbers), m.unknown...)
}

func (m *SoftwareUpdatePartNumberResponse) unmarshal(b []byte) error {
	return walk("SoftwareUpdatePartNumberResponse", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decStrings(&m.PartNumbers, f)
		}
		return false, nil
	})
}
