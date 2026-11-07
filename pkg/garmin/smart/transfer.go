// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

// PullItemDataRequest is GDI.Proto.FileAccess.PullItemDataRequest (FileAccess field 1, host
// to watch). Garmin Connect sends {Item: {UID}, Priority: 24, SupportedTransports:
// [TransportMultilinkPipe], Offset: 0, RequestedCompressionWindow: 15}.
type PullItemDataRequest struct {
	Item                       *ItemReference      // 1
	Priority                   *uint32             // 2
	SupportedTransports        []TransportProtocol // 3
	Offset                     *uint64             // 4: clear-file resume offset
	RequestedCompressionWindow *uint32             // 5

	unknown []byte
}

// ParsePullItemDataRequest decodes a PullItemDataRequest message.
func ParsePullItemDataRequest(b []byte) (PullItemDataRequest, error) {
	return parse[PullItemDataRequest](b)
}

// Marshal encodes the message.
func (m PullItemDataRequest) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.Item)
	b = encU32(b, 2, m.Priority)
	b = encEnums(b, 3, m.SupportedTransports)
	b = encU64(b, 4, m.Offset)
	b = encU32(b, 5, m.RequestedCompressionWindow)
	return append(b, m.unknown...)
}

func (m *PullItemDataRequest) unmarshal(b []byte) error {
	return walk("PullItemDataRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.Item, f)
		case 2:
			return true, decU32(&m.Priority, f)
		case 3:
			return true, decEnums(&m.SupportedTransports, f)
		case 4:
			return true, decU64(&m.Offset, f)
		case 5:
			return true, decU32(&m.RequestedCompressionWindow, f)
		}
		return false, nil
	})
}

// PullItemDataResponse is GDI.Proto.FileAccess.PullItemDataResponse (FileAccess field 2,
// watch to host). On success the item bytes follow on a Multi-Link pipe configured with
// TransferHandle; CompressionWindow present means the bytes are a zlib stream.
type PullItemDataResponse struct {
	Result                  *ItemAccessResult  // 1
	Transport               *TransportProtocol // 2
	TransferHandle          *uint32            // 3
	FilePath                *string            // 4
	ErrorCode               *int32             // 5: sint32
	ErrorSource             *uint32            // 6
	CompressionWindow       *uint32            // 7: zlib window bits, e.g. 11
	DataSpecificErrorSource *uint32            // 8
	DataSpecificErrorCode   *int32             // 9: sint32

	unknown []byte
}

// ParsePullItemDataResponse decodes a PullItemDataResponse message.
func ParsePullItemDataResponse(b []byte) (PullItemDataResponse, error) {
	return parse[PullItemDataResponse](b)
}

// Marshal encodes the message.
func (m PullItemDataResponse) Marshal() []byte {
	var b []byte
	b = encEnum(b, 1, m.Result)
	b = encEnum(b, 2, m.Transport)
	b = encU32(b, 3, m.TransferHandle)
	b = encString(b, 4, m.FilePath)
	b = encS32(b, 5, m.ErrorCode)
	b = encU32(b, 6, m.ErrorSource)
	b = encU32(b, 7, m.CompressionWindow)
	b = encU32(b, 8, m.DataSpecificErrorSource)
	b = encS32(b, 9, m.DataSpecificErrorCode)
	return append(b, m.unknown...)
}

func (m *PullItemDataResponse) unmarshal(b []byte) error {
	return walk("PullItemDataResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Result, f)
		case 2:
			return true, decEnum(&m.Transport, f)
		case 3:
			return true, decU32(&m.TransferHandle, f)
		case 4:
			return true, decString(&m.FilePath, f)
		case 5:
			return true, decS32(&m.ErrorCode, f)
		case 6:
			return true, decU32(&m.ErrorSource, f)
		case 7:
			return true, decU32(&m.CompressionWindow, f)
		case 8:
			return true, decU32(&m.DataSpecificErrorSource, f)
		case 9:
			return true, decS32(&m.DataSpecificErrorCode, f)
		}
		return false, nil
	})
}

// PushItemDataRequest is GDI.Proto.FileAccess.PushItemDataRequest (FileAccess field 3, host
// to watch). Unlike the pull request it has no offset field: the window is field 4.
type PushItemDataRequest struct {
	Item                       *ItemReference      // 1
	Priority                   *uint32             // 2
	SupportedTransports        []TransportProtocol // 3
	RequestedCompressionWindow *uint32             // 4

	unknown []byte
}

// ParsePushItemDataRequest decodes a PushItemDataRequest message.
func ParsePushItemDataRequest(b []byte) (PushItemDataRequest, error) {
	return parse[PushItemDataRequest](b)
}

// Marshal encodes the message.
func (m PushItemDataRequest) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.Item)
	b = encU32(b, 2, m.Priority)
	b = encEnums(b, 3, m.SupportedTransports)
	b = encU32(b, 4, m.RequestedCompressionWindow)
	return append(b, m.unknown...)
}

func (m *PushItemDataRequest) unmarshal(b []byte) error {
	return walk("PushItemDataRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.Item, f)
		case 2:
			return true, decU32(&m.Priority, f)
		case 3:
			return true, decEnums(&m.SupportedTransports, f)
		case 4:
			return true, decU32(&m.RequestedCompressionWindow, f)
		}
		return false, nil
	})
}

// PushItemDataResponse is GDI.Proto.FileAccess.PushItemDataResponse (FileAccess field 4,
// watch to host). Its field numbers are shifted by one against PullItemDataResponse because
// of Offset (field 2).
type PushItemDataResponse struct {
	Result                  *ItemAccessResult  // 1
	Offset                  *uint64            // 2
	Transport               *TransportProtocol // 3
	TransferHandle          *uint32            // 4
	FilePath                *string            // 5
	ErrorCode               *int32             // 6: sint32
	ErrorSource             *uint32            // 7
	CompressionWindow       *uint32            // 8
	DataSpecificErrorSource *uint32            // 9
	DataSpecificErrorCode   *int32             // 10: sint32

	unknown []byte
}

// ParsePushItemDataResponse decodes a PushItemDataResponse message.
func ParsePushItemDataResponse(b []byte) (PushItemDataResponse, error) {
	return parse[PushItemDataResponse](b)
}

// Marshal encodes the message.
func (m PushItemDataResponse) Marshal() []byte {
	var b []byte
	b = encEnum(b, 1, m.Result)
	b = encU64(b, 2, m.Offset)
	b = encEnum(b, 3, m.Transport)
	b = encU32(b, 4, m.TransferHandle)
	b = encString(b, 5, m.FilePath)
	b = encS32(b, 6, m.ErrorCode)
	b = encU32(b, 7, m.ErrorSource)
	b = encU32(b, 8, m.CompressionWindow)
	b = encU32(b, 9, m.DataSpecificErrorSource)
	b = encS32(b, 10, m.DataSpecificErrorCode)
	return append(b, m.unknown...)
}

func (m *PushItemDataResponse) unmarshal(b []byte) error {
	return walk("PushItemDataResponse", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Result, f)
		case 2:
			return true, decU64(&m.Offset, f)
		case 3:
			return true, decEnum(&m.Transport, f)
		case 4:
			return true, decU32(&m.TransferHandle, f)
		case 5:
			return true, decString(&m.FilePath, f)
		case 6:
			return true, decS32(&m.ErrorCode, f)
		case 7:
			return true, decU32(&m.ErrorSource, f)
		case 8:
			return true, decU32(&m.CompressionWindow, f)
		case 9:
			return true, decU32(&m.DataSpecificErrorSource, f)
		case 10:
			return true, decS32(&m.DataSpecificErrorCode, f)
		}
		return false, nil
	})
}

// TransferStatusRequest is GDI.Proto.FileAccess.TransferStatusRequest (FileAccess field 5,
// watch to host). It needs a TransferStatusResponse; FailureReason present means the
// transfer failed.
type TransferStatusRequest struct {
	UID                 *UUID          // 1
	TransferHandle      *uint32        // 2
	FailureReason       *FailureReason // 3
	TransportStatus     *uint32        // 4
	TransportProviderID *uint32        // 5
	TransportErrorCode  *uint32        // 6
	NewPriority         *uint32        // 7

	unknown []byte
}

// ParseTransferStatusRequest decodes a TransferStatusRequest message.
func ParseTransferStatusRequest(b []byte) (TransferStatusRequest, error) {
	return parse[TransferStatusRequest](b)
}

// Marshal encodes the message.
func (m TransferStatusRequest) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.UID)
	b = encU32(b, 2, m.TransferHandle)
	b = encEnum(b, 3, m.FailureReason)
	b = encU32(b, 4, m.TransportStatus)
	b = encU32(b, 5, m.TransportProviderID)
	b = encU32(b, 6, m.TransportErrorCode)
	b = encU32(b, 7, m.NewPriority)
	return append(b, m.unknown...)
}

func (m *TransferStatusRequest) unmarshal(b []byte) error {
	return walk("TransferStatusRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decU32(&m.TransferHandle, f)
		case 3:
			return true, decEnum(&m.FailureReason, f)
		case 4:
			return true, decU32(&m.TransportStatus, f)
		case 5:
			return true, decU32(&m.TransportProviderID, f)
		case 6:
			return true, decU32(&m.TransportErrorCode, f)
		case 7:
			return true, decU32(&m.NewPriority, f)
		}
		return false, nil
	})
}

// TransferStatusResponse is GDI.Proto.FileAccess.TransferStatusResponse (FileAccess field 21,
// host to watch). Garmin Connect answers with NextTransferPriority 24, Gadgetbridge with 1.
type TransferStatusResponse struct {
	NextTransferPriority *uint32 // 1

	unknown []byte
}

// ParseTransferStatusResponse decodes a TransferStatusResponse message.
func ParseTransferStatusResponse(b []byte) (TransferStatusResponse, error) {
	return parse[TransferStatusResponse](b)
}

// Marshal encodes the message.
func (m TransferStatusResponse) Marshal() []byte {
	return append(encU32(nil, 1, m.NextTransferPriority), m.unknown...)
}

func (m *TransferStatusResponse) unmarshal(b []byte) error {
	return walk("TransferStatusResponse", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decU32(&m.NextTransferPriority, f)
		}
		return false, nil
	})
}

// PriorityUpdateRequest is GDI.Proto.FileAccess.PriorityUpdateRequest (FileAccess field 6,
// host to watch).
type PriorityUpdateRequest struct {
	TransferHandle *uint32 // 1
	NewPriority    *uint32 // 2

	unknown []byte
}

// ParsePriorityUpdateRequest decodes a PriorityUpdateRequest message.
func ParsePriorityUpdateRequest(b []byte) (PriorityUpdateRequest, error) {
	return parse[PriorityUpdateRequest](b)
}

// Marshal encodes the message.
func (m PriorityUpdateRequest) Marshal() []byte {
	b := encU32(nil, 1, m.TransferHandle)
	b = encU32(b, 2, m.NewPriority)
	return append(b, m.unknown...)
}

func (m *PriorityUpdateRequest) unmarshal(b []byte) error {
	return walk("PriorityUpdateRequest", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decU32(&m.TransferHandle, f)
		case 2:
			return true, decU32(&m.NewPriority, f)
		}
		return false, nil
	})
}

// PriorityUpdateResponse is GDI.Proto.FileAccess.PriorityUpdateResponse (FileAccess field 7,
// watch to host).
type PriorityUpdateResponse struct {
	Status *PriorityUpdateStatus // 1

	unknown []byte
}

// ParsePriorityUpdateResponse decodes a PriorityUpdateResponse message.
func ParsePriorityUpdateResponse(b []byte) (PriorityUpdateResponse, error) {
	return parse[PriorityUpdateResponse](b)
}

// Marshal encodes the message.
func (m PriorityUpdateResponse) Marshal() []byte {
	return append(encEnum(nil, 1, m.Status), m.unknown...)
}

func (m *PriorityUpdateResponse) unmarshal(b []byte) error {
	return walk("PriorityUpdateResponse", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decEnum(&m.Status, f)
		}
		return false, nil
	})
}

// CancelTransferRequest is GDI.Proto.FileAccess.CancelTransferRequest (FileAccess field 18,
// host to watch).
type CancelTransferRequest struct {
	TransferHandle *uint32 // 1

	unknown []byte
}

// ParseCancelTransferRequest decodes a CancelTransferRequest message.
func ParseCancelTransferRequest(b []byte) (CancelTransferRequest, error) {
	return parse[CancelTransferRequest](b)
}

// Marshal encodes the message.
func (m CancelTransferRequest) Marshal() []byte {
	return append(encU32(nil, 1, m.TransferHandle), m.unknown...)
}

func (m *CancelTransferRequest) unmarshal(b []byte) error {
	return walk("CancelTransferRequest", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decU32(&m.TransferHandle, f)
		}
		return false, nil
	})
}

// CancelTransferResponse is GDI.Proto.FileAccess.CancelTransferResponse (FileAccess field 19,
// watch to host).
type CancelTransferResponse struct {
	Status *CancelTransferStatus // 1

	unknown []byte
}

// ParseCancelTransferResponse decodes a CancelTransferResponse message.
func ParseCancelTransferResponse(b []byte) (CancelTransferResponse, error) {
	return parse[CancelTransferResponse](b)
}

// Marshal encodes the message.
func (m CancelTransferResponse) Marshal() []byte {
	return append(encEnum(nil, 1, m.Status), m.unknown...)
}

func (m *CancelTransferResponse) unmarshal(b []byte) error {
	return walk("CancelTransferResponse", b, &m.unknown, func(f *field) (bool, error) {
		if f.num == 1 {
			return true, decEnum(&m.Status, f)
		}
		return false, nil
	})
}

// ResourceUpdateNotification is GDI.Proto.FileAccess.ResourceUpdateNotification (FileAccess
// field 20, watch to host). Informational; no reply.
type ResourceUpdateNotification struct {
	Status          *ResourceUpdateStatus // 1
	CurrentPriority *uint32               // 2

	unknown []byte
}

// ParseResourceUpdateNotification decodes a ResourceUpdateNotification message.
func ParseResourceUpdateNotification(b []byte) (ResourceUpdateNotification, error) {
	return parse[ResourceUpdateNotification](b)
}

// Marshal encodes the message.
func (m ResourceUpdateNotification) Marshal() []byte {
	b := encEnum(nil, 1, m.Status)
	b = encU32(b, 2, m.CurrentPriority)
	return append(b, m.unknown...)
}

func (m *ResourceUpdateNotification) unmarshal(b []byte) error {
	return walk("ResourceUpdateNotification", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Status, f)
		case 2:
			return true, decU32(&m.CurrentPriority, f)
		}
		return false, nil
	})
}
