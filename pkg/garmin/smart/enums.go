// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "fmt"

// Enums of gdi_file_access.proto. String returns the proto value name, so log lines match
// the schema; a value the schema does not define prints as "Type(n)".

func enumString[E ~uint32](typ string, v E, names map[E]string) string {
	if n, ok := names[v]; ok {
		return n
	}
	return fmt.Sprintf("%s(%d)", typ, uint32(v))
}

// ItemAccessResult is GDI.Proto.FileAccess.ItemAccessResult, the result of a pull or push.
type ItemAccessResult uint32

// ItemAccessResult values.
const (
	ItemAccessSuccess                ItemAccessResult = 0
	ItemAccessItemTypeNotSupported   ItemAccessResult = 1
	ItemAccessNoSupportedTransport   ItemAccessResult = 2
	ItemAccessNoAvailableResources   ItemAccessResult = 3
	ItemAccessInternalError          ItemAccessResult = 4
	ItemAccessInvalidParams          ItemAccessResult = 5
	ItemAccessInvalidOffset          ItemAccessResult = 6
	ItemAccessAmbiguousDataType      ItemAccessResult = 7
	ItemAccessUnknownItem            ItemAccessResult = 8
	ItemAccessNoSpace                ItemAccessResult = 9
	ItemAccessDataTypeError          ItemAccessResult = 10
	ItemAccessConflictingPushRequest ItemAccessResult = 11
)

var itemAccessResultNames = map[ItemAccessResult]string{
	0: "SUCCESS", 1: "ITEM_TYPE_NOT_SUPPORTED", 2: "NO_SUPPORTED_TRANSPORT",
	3: "NO_AVAILABLE_RESOURCES", 4: "INTERNAL_ERROR", 5: "INVALID_PARAMS", 6: "INVALID_OFFSET",
	7: "AMBIGUOUS_DATA_TYPE", 8: "UNKNOWN_ITEM", 9: "NO_SPACE", 10: "DATA_TYPE_ERROR",
	11: "CONFLICTING_PUSH_REQUEST",
}

func (v ItemAccessResult) String() string {
	return enumString("ItemAccessResult", v, itemAccessResultNames)
}

// TransportProtocol is GDI.Proto.FileAccess.TransportProtocol.
type TransportProtocol uint32

// TransportMultilinkPipe moves the item bytes over a Multi-Link file-transfer pipe.
const TransportMultilinkPipe TransportProtocol = 0

var transportProtocolNames = map[TransportProtocol]string{0: "MULTILINK_TRANSPORT_PIPE"}

func (v TransportProtocol) String() string {
	return enumString("TransportProtocol", v, transportProtocolNames)
}

// TransferDirection is GDI.Proto.FileAccess.TransferDirection.
type TransferDirection uint32

// TransferDirection values.
const (
	DirectionPush TransferDirection = 0
	DirectionPull TransferDirection = 1
)

var transferDirectionNames = map[TransferDirection]string{0: "PUSH", 1: "PULL"}

func (v TransferDirection) String() string {
	return enumString("TransferDirection", v, transferDirectionNames)
}

// TransferPriorityLevel is GDI.Proto.FileAccess.TransferPriorityLevel: the bounds of the
// priority bands. The priority fields themselves are plain uint32 (Garmin Connect sends 24
// for pulls and 34 for pushes).
type TransferPriorityLevel uint32

// TransferPriorityLevel values.
const (
	PriorityLowestStart        TransferPriorityLevel = 0
	PriorityLowestEnd          TransferPriorityLevel = 9
	PriorityUtilityStart       TransferPriorityLevel = 10
	PriorityUtilityEnd         TransferPriorityLevel = 19
	PriorityStandardStart      TransferPriorityLevel = 20
	PriorityStandardEnd        TransferPriorityLevel = 29
	PriorityUserInitiatedStart TransferPriorityLevel = 30
	PriorityUserInitiatedEnd   TransferPriorityLevel = 39
	PriorityHighestStart       TransferPriorityLevel = 40
	PriorityHighestEnd         TransferPriorityLevel = 49
)

var transferPriorityLevelNames = map[TransferPriorityLevel]string{
	0: "LOWEST_START", 9: "LOWEST_END", 10: "UTILITY_START", 19: "UTILITY_END",
	20: "STANDARD_START", 29: "STANDARD_END", 30: "USER_INITIATED_START",
	39: "USER_INITIATED_END", 40: "HIGHEST_START", 49: "HIGHEST_END",
}

func (v TransferPriorityLevel) String() string {
	return enumString("TransferPriorityLevel", v, transferPriorityLevelNames)
}

// DataFormat is GDI.Proto.FileAccess.DataType.Format.
type DataFormat uint32

// DataFormat values.
const (
	FormatGDXMLDataType   DataFormat = 0
	FormatSoftwareUpdate  DataFormat = 1
	FormatSpecialCase     DataFormat = 2
	FormatGDXMLFullFile   DataFormat = 3
	FormatDataXfer        DataFormat = 4
	FormatGDXMLUpdateFile DataFormat = 5
)

var dataFormatNames = map[DataFormat]string{
	0: "GDXML_DATA_TYPE", 1: "SOFTWARE_UPDATE", 2: "SPECIAL_CASE", 3: "GDXML_FULL_FILE",
	4: "DATA_XFER", 5: "GDXML_UPDATE_FILE",
}

func (v DataFormat) String() string { return enumString("DataFormat", v, dataFormatNames) }

// SpecialCase is GDI.Proto.FileAccess.DataType.SpecialCase.
type SpecialCase uint32

// SpecialCaseCounterByteTest is the only SpecialCase value.
const SpecialCaseCounterByteTest SpecialCase = 1

var specialCaseNames = map[SpecialCase]string{1: "COUNTER_BYTE_TEST"}

func (v SpecialCase) String() string { return enumString("SpecialCase", v, specialCaseNames) }

// DeleteItemResult is GDI.Proto.FileAccess.DeleteItemResponse.Result.
type DeleteItemResult uint32

// DeleteItemResult values.
const (
	DeleteItemSuccess               DeleteItemResult = 0
	DeleteItemDoesNotExist          DeleteItemResult = 1
	DeleteItemCannotBeDeleted       DeleteItemResult = 2
	DeleteItemTransferInProgress    DeleteItemResult = 3
	DeleteItemFailed                DeleteItemResult = 4
	DeleteItemInvalidParams         DeleteItemResult = 5
	DeleteItemOperationNotSupported DeleteItemResult = 6
)

var deleteItemResultNames = map[DeleteItemResult]string{
	0: "SUCCESS", 1: "ITEM_DOES_NOT_EXIST", 2: "ITEM_CANNOT_BE_DELETED", 3: "TRANSFER_IN_PROGRESS",
	4: "DELETE_FAILED", 5: "INVALID_PARAMS", 6: "OPERATION_NOT_SUPPORTED",
}

func (v DeleteItemResult) String() string {
	return enumString("DeleteItemResult", v, deleteItemResultNames)
}

// ChecksumResult is GDI.Proto.FileAccess.GetItemChecksumResponse.Result.
type ChecksumResult uint32

// ChecksumResult values.
const (
	ChecksumResultUnspecified          ChecksumResult = 0
	ChecksumResultSuccess              ChecksumResult = 1
	ChecksumResultFailItemDoesNotExist ChecksumResult = 2
	ChecksumResultFailItemSizeTooLarge ChecksumResult = 3
	ChecksumResultFailInternalError    ChecksumResult = 4
	ChecksumResultFailDeviceIsBusy     ChecksumResult = 5
)

var checksumResultNames = map[ChecksumResult]string{
	0: "UNSPECIFIED", 1: "SUCCESS", 2: "FAIL_ITEM_DOES_NOT_EXIST", 3: "FAIL_ITEM_SIZE_TOO_LARGE",
	4: "FAIL_INTERNAL_ERROR", 5: "FAIL_DEVICE_IS_BUSY",
}

func (v ChecksumResult) String() string {
	return enumString("ChecksumResult", v, checksumResultNames)
}

// ChecksumMethod is GDI.Proto.FileAccess.Capabilities.ChecksumMethod.
type ChecksumMethod uint32

// ChecksumMethod values.
const (
	ChecksumMethodNotSupported ChecksumMethod = 0
	ChecksumMethodTruncatedMD5 ChecksumMethod = 1
)

var checksumMethodNames = map[ChecksumMethod]string{0: "NOT_SUPPORTED", 1: "TRUNCATED_MD5"}

func (v ChecksumMethod) String() string {
	return enumString("ChecksumMethod", v, checksumMethodNames)
}

// MetadataType is GDI.Proto.FileAccess.ItemListRequest.MetaDataType.
type MetadataType uint32

// MetadataModifiedTime asks for ItemReference.ModifiedTime in a listing.
const MetadataModifiedTime MetadataType = 0

var metadataTypeNames = map[MetadataType]string{0: "MODIFIED_TIME"}

func (v MetadataType) String() string { return enumString("MetadataType", v, metadataTypeNames) }

// ItemListStatus is GDI.Proto.FileAccess.ItemListResponse.Status.
type ItemListStatus uint32

// ItemListStatus values.
const (
	ItemListSuccess                 ItemListStatus = 0
	ItemListFailUnknownSessionID    ItemListStatus = 1
	ItemListFailTooManyExcludeFlags ItemListStatus = 2
	ItemListFailTooManyGetFlags     ItemListStatus = 3
	ItemListFailOther               ItemListStatus = 4
	ItemListFailTooManyIncludeTypes ItemListStatus = 5
	ItemListFailInvalidDataType     ItemListStatus = 6
)

var itemListStatusNames = map[ItemListStatus]string{
	0: "SUCCESS", 1: "FAIL_UNKNOWN_SESSION_ID", 2: "FAIL_TOO_MANY_EXCLUDE_FLAGS",
	3: "FAIL_TOO_MANY_GET_FLAGS", 4: "FAIL_OTHER", 5: "FAIL_TOO_MANY_INCLUDE_TYPES",
	6: "FAIL_INVALID_DATA_TYPE",
}

func (v ItemListStatus) String() string {
	return enumString("ItemListStatus", v, itemListStatusNames)
}

// Urgency is GDI.Proto.FileAccess.ItemReference.Urgency.
type Urgency uint32

// Urgency values.
const (
	UrgencyImmediate Urgency = 0
	UrgencyNormal    Urgency = 1
	UrgencySlow      Urgency = 2
)

var urgencyNames = map[Urgency]string{0: "IMMEDIATE", 1: "NORMAL", 2: "SLOW"}

func (v Urgency) String() string { return enumString("Urgency", v, urgencyNames) }

// ModifyFlagsStatus is GDI.Proto.FileAccess.ModifyFlagsResponse.Status.
type ModifyFlagsStatus uint32

// ModifyFlagsStatus values.
const (
	ModifyFlagsSuccess              ModifyFlagsStatus = 0
	ModifyFlagsFailItemNotProvided  ModifyFlagsStatus = 1
	ModifyFlagsFailItemNotFound     ModifyFlagsStatus = 2
	ModifyFlagsFailFlagsNotProvided ModifyFlagsStatus = 3
	ModifyFlagsFailMaxFlagsReached  ModifyFlagsStatus = 4
	ModifyFlagsFailOther            ModifyFlagsStatus = 5
)

var modifyFlagsStatusNames = map[ModifyFlagsStatus]string{
	0: "SUCCESS", 1: "FAIL_ITEM_NOT_PROVIDED", 2: "FAIL_ITEM_NOT_FOUND",
	3: "FAIL_FLAGS_NOT_PROVIDED", 4: "FAIL_MAX_FLAGS_REACHED", 5: "FAIL_OTHER",
}

func (v ModifyFlagsStatus) String() string {
	return enumString("ModifyFlagsStatus", v, modifyFlagsStatusNames)
}

// PriorityUpdateStatus is GDI.Proto.FileAccess.PriorityUpdateResponse.Status.
type PriorityUpdateStatus uint32

// PriorityUpdateStatus values.
const (
	PriorityUpdateSuccess         PriorityUpdateStatus = 0
	PriorityUpdateInvalidPriority PriorityUpdateStatus = 1
	PriorityUpdateUnknownTransfer PriorityUpdateStatus = 2
)

var priorityUpdateStatusNames = map[PriorityUpdateStatus]string{
	0: "SUCCESS", 1: "INVALID_PRIORITY", 2: "UNKNOWN_TRANSFER",
}

func (v PriorityUpdateStatus) String() string {
	return enumString("PriorityUpdateStatus", v, priorityUpdateStatusNames)
}

// CancelTransferStatus is GDI.Proto.FileAccess.CancelTransferResponse.Status.
type CancelTransferStatus uint32

// CancelTransferStatus values.
const (
	CancelTransferSuccess         CancelTransferStatus = 0
	CancelTransferUnknownTransfer CancelTransferStatus = 1
)

var cancelTransferStatusNames = map[CancelTransferStatus]string{0: "SUCCESS", 1: "UNKNOWN_TRANSFER"}

func (v CancelTransferStatus) String() string {
	return enumString("CancelTransferStatus", v, cancelTransferStatusNames)
}

// ResourceUpdateStatus is GDI.Proto.FileAccess.ResourceUpdateNotification.Status. The proto
// defines no zero value.
type ResourceUpdateStatus uint32

// ResourceUpdateStatus values.
const (
	ResourceCurrentPriorityLowered ResourceUpdateStatus = 1
	ResourceFileSpaceFreed         ResourceUpdateStatus = 2
)

var resourceUpdateStatusNames = map[ResourceUpdateStatus]string{
	1: "CURRENT_PRIORITY_LOWERED", 2: "FILE_SPACE_FREED",
}

func (v ResourceUpdateStatus) String() string {
	return enumString("ResourceUpdateStatus", v, resourceUpdateStatusNames)
}

// FailureReason is GDI.Proto.FileAccess.TransferStatusRequest.FailureReason.
type FailureReason uint32

// FailureReason values.
const (
	FailureTransportFailed               FailureReason = 0
	FailureHigherPriorityRequestReceived FailureReason = 1
	FailureItemDeleted                   FailureReason = 2
	FailureCancelledByClient             FailureReason = 3
	FailureCancelledByDevice             FailureReason = 4
	FailureCompressionFailed             FailureReason = 5
)

var failureReasonNames = map[FailureReason]string{
	0: "TRANSPORT_FAILED", 1: "HIGHER_PRIORITY_REQUEST_RECEIVED", 2: "ITEM_DELETED",
	3: "CANCELLED_BY_CLIENT", 4: "CANCELLED_BY_DEVICE", 5: "COMPRESSION_FAILED",
}

func (v FailureReason) String() string { return enumString("FailureReason", v, failureReasonNames) }

// Requestor is GDI.Proto.FileAccess.SoftwareUpdatePartNumberRequest.Requestor.
type Requestor uint32

// Requestor values.
const (
	RequestorDisplay Requestor = 0
	RequestorApp     Requestor = 1
)

var requestorNames = map[Requestor]string{0: "DISPLAY", 1: "APP"}

func (v Requestor) String() string { return enumString("Requestor", v, requestorNames) }
