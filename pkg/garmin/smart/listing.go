// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

// ItemListRequest is GDI.Proto.FileAccess.ItemListRequest (FileAccess field 9, host to
// watch). Garmin Connect's first listing is {ExcludeAllFlags: [SyncedFlag], GetFlags:
// [SyncedFlag]}; while a response carries SessionID it continues with {SessionID} alone.
type ItemListRequest struct {
	SessionID         *uint32        // 1: continue the listing cursor of a previous response
	TransactionID     *uint32        // 2: previous NextTransactionID, for an incremental listing
	MaxItemCount      *uint32        // 3
	ExcludeAllFlags   []UUID         // 4
	GetFlags          []UUID         // 5
	IncludedDataTypes []DataType     // 6
	RequestedMetadata []MetadataType // 7

	unknown []byte
}

// ParseItemListRequest decodes an ItemListRequest message.
func ParseItemListRequest(b []byte) (ItemListRequest, error) { return parse[ItemListRequest](b) }

// Marshal encodes the message.
func (m ItemListRequest) Marshal() []byte {
	var b []byte
	b = encU32(b, 1, m.SessionID)
	b = encU32(b, 2, m.TransactionID)
	b = encU32(b, 3, m.MaxItemCount)
	b = encMsgs(b, 4, m.ExcludeAllFlags)
	b = encMsgs(b, 5, m.GetFlags)
	b = encMsgs(b, 6, m.IncludedDataTypes)
	b = encEnums(b, 7, m.RequestedMetadata)
	return append(b, m.unknown...)
}

func (m *ItemListRequest) unmarshal(b []byte) error {
	return walk("ItemListRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decU32(&m.SessionID, f)
		case 2:
			return true, decU32(&m.TransactionID, f)
		case 3:
			return true, decU32(&m.MaxItemCount, f)
		case 4:
			return true, decMsgs(&m.ExcludeAllFlags, f)
		case 5:
			return true, decMsgs(&m.GetFlags, f)
		case 6:
			return true, decMsgs(&m.IncludedDataTypes, f)
		case 7:
			return true, decEnums(&m.RequestedMetadata, f)
		}
		return false, nil
	})
}

// ItemListResponse is GDI.Proto.FileAccess.ItemListResponse (FileAccess field 10, watch to
// host). SessionID is present only while more pages of this listing remain; the last page
// carries NextTransactionID instead.
type ItemListResponse struct {
	Status             *ItemListStatus // 1
	SessionID          *uint32         // 2: present means more pages follow
	NextTransactionID  *uint32         // 3
	Items              []ItemReference // 4
	ErrorCode          *uint32         // 5
	AbandonedSessionID *uint32         // 6
	MalformedDataType  *DataType       // 7

	unknown []byte
}

// ParseItemListResponse decodes an ItemListResponse message.
func ParseItemListResponse(b []byte) (ItemListResponse, error) {
	return parse[ItemListResponse](b)
}

// Marshal encodes the message.
func (m ItemListResponse) Marshal() []byte {
	var b []byte
	b = encEnum(b, 1, m.Status)
	b = encU32(b, 2, m.SessionID)
	b = encU32(b, 3, m.NextTransactionID)
	b = encMsgs(b, 4, m.Items)
	b = encU32(b, 5, m.ErrorCode)
	b = encU32(b, 6, m.AbandonedSessionID)
	b = encMsg(b, 7, m.MalformedDataType)
	return append(b, m.unknown...)
}

func (m *ItemListResponse) unmarshal(b []byte) error {
	return walk("ItemListResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Status, f)
		case 2:
			return true, decU32(&m.SessionID, f)
		case 3:
			return true, decU32(&m.NextTransactionID, f)
		case 4:
			return true, decMsgs(&m.Items, f)
		case 5:
			return true, decU32(&m.ErrorCode, f)
		case 6:
			return true, decU32(&m.AbandonedSessionID, f)
		case 7:
			return true, decMsg(&m.MalformedDataType, f)
		}
		return false, nil
	})
}

// ItemListCancelNotification is GDI.Proto.FileAccess.ItemListCancelNotification (FileAccess
// field 11, watch to host): the watch dropped a listing cursor.
type ItemListCancelNotification struct {
	SessionID *uint32 // 1

	unknown []byte
}

// ParseItemListCancelNotification decodes an ItemListCancelNotification message.
func ParseItemListCancelNotification(b []byte) (ItemListCancelNotification, error) {
	return parse[ItemListCancelNotification](b)
}

// Marshal encodes the message.
func (m ItemListCancelNotification) Marshal() []byte {
	return append(encU32(nil, 1, m.SessionID), m.unknown...)
}

func (m *ItemListCancelNotification) unmarshal(b []byte) error {
	return walk("ItemListCancelNotification", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decU32(&m.SessionID, f)
		}
		return false, nil
	})
}

// ItemAddedNotification is GDI.Proto.FileAccess.ItemAddedNotification (FileAccess field 12,
// watch to host): new items appeared on the watch.
type ItemAddedNotification struct {
	Items []ItemReference // 1

	unknown []byte
}

// ParseItemAddedNotification decodes an ItemAddedNotification message.
func ParseItemAddedNotification(b []byte) (ItemAddedNotification, error) {
	return parse[ItemAddedNotification](b)
}

// Marshal encodes the message.
func (m ItemAddedNotification) Marshal() []byte {
	return append(encMsgs(nil, 1, m.Items), m.unknown...)
}

func (m *ItemAddedNotification) unmarshal(b []byte) error {
	return walk("ItemAddedNotification", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decMsgs(&m.Items, f)
		}
		return false, nil
	})
}

// SyncButtonNotification is GDI.Proto.FileAccess.SyncButtonNotification (FileAccess field
// 22, watch to host): the user asked for a sync. It has no fields.
type SyncButtonNotification struct {
	unknown []byte
}

// ParseSyncButtonNotification decodes a SyncButtonNotification message.
func ParseSyncButtonNotification(b []byte) (SyncButtonNotification, error) {
	return parse[SyncButtonNotification](b)
}

// Marshal encodes the message.
func (m SyncButtonNotification) Marshal() []byte { return append([]byte(nil), m.unknown...) }

func (m *SyncButtonNotification) unmarshal(b []byte) error {
	return walk("SyncButtonNotification", b, &m.unknown, func(*field) (bool, error) {
		return false, nil
	})
}
