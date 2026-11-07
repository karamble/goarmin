// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "fmt"

// UUID is GDI.Proto.DataTypes.UUID: two required fixed64 halves. It identifies items and
// serves as a flag value. Marshal always emits both halves. UUID is a comparable value, so
// unlike the other messages it drops unknown fields, and a missing half decodes as zero.
type UUID struct {
	MSB uint64 // most_significant = 1
	LSB uint64 // least_significant = 2
}

// SyncedFlag is the flag Garmin Connect (and Gadgetbridge) set on an item once it has been
// stored, and exclude from listings to get only new items.
var SyncedFlag = UUID{MSB: 0xA5A5, LSB: 0xA5A5}

// String formats the UUID as two 16-digit hex halves, "msb-lsb".
func (u UUID) String() string { return fmt.Sprintf("%016x-%016x", u.MSB, u.LSB) }

// ParseUUID decodes a UUID message.
func ParseUUID(b []byte) (UUID, error) { return parse[UUID](b) }

// Marshal encodes the UUID, both halves always present.
func (u UUID) Marshal() []byte {
	b := encFixed64v(nil, 1, u.MSB)
	return encFixed64v(b, 2, u.LSB)
}

func (u *UUID) unmarshal(b []byte) error {
	return walk("UUID", b, nil, func(f *field) (bool, error) {
		switch f.num {
		case 1, 2:
			var v *uint64
			if err := decFixed64(&v, f); err != nil {
				return true, err
			}
			if f.num == 1 {
				u.MSB = *v
			} else {
				u.LSB = *v
			}
			return true, nil
		}
		return false, nil
	})
}

// DataType is GDI.Proto.FileAccess.DataType, the type of an item. In a listing only the first
// item of a type carries GDXMLString; later items carry only the session-scoped
// GDXMLStringKey.
type DataType struct {
	Format              *DataFormat  // 1
	GDXMLString         *string      // 2: type name, e.g. "FIT_TYPE_32"
	GDXMLStringKey      *uint32      // 3: per-session key for GDXMLString
	GDXMLFileIdentifier *string      // 4
	SoftwareUpdatePath  *string      // 5
	SpecialCase         *SpecialCase // 6
	UpdatePartNumber    *string      // 7

	unknown []byte
}

// ParseDataType decodes a DataType message.
func ParseDataType(b []byte) (DataType, error) { return parse[DataType](b) }

// Marshal encodes the message.
func (m DataType) Marshal() []byte {
	var b []byte
	b = encEnum(b, 1, m.Format)
	b = encString(b, 2, m.GDXMLString)
	b = encU32(b, 3, m.GDXMLStringKey)
	b = encString(b, 4, m.GDXMLFileIdentifier)
	b = encString(b, 5, m.SoftwareUpdatePath)
	b = encEnum(b, 6, m.SpecialCase)
	b = encString(b, 7, m.UpdatePartNumber)
	return append(b, m.unknown...)
}

func (m *DataType) unmarshal(b []byte) error {
	return walk("DataType", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decEnum(&m.Format, f)
		case 2:
			return true, decString(&m.GDXMLString, f)
		case 3:
			return true, decU32(&m.GDXMLStringKey, f)
		case 4:
			return true, decString(&m.GDXMLFileIdentifier, f)
		case 5:
			return true, decString(&m.SoftwareUpdatePath, f)
		case 6:
			return true, decEnum(&m.SpecialCase, f)
		case 7:
			return true, decString(&m.UpdatePartNumber, f)
		}
		return false, nil
	})
}

// ItemReference is GDI.Proto.FileAccess.ItemReference: one item (file) on the watch.
type ItemReference struct {
	UID            *UUID     // 1
	DataType       *DataType // 2
	DataSize       *uint64   // 3: clear (decompressed) size
	RequestedFlags *uint32   // 4
	TransactionID  *uint32   // 5
	Urgency        *Urgency  // 6
	ModifiedTime   *uint32   // 7: Garmin timestamp, when MODIFIED_TIME was requested

	unknown []byte
}

// ParseItemReference decodes an ItemReference message.
func ParseItemReference(b []byte) (ItemReference, error) { return parse[ItemReference](b) }

// Marshal encodes the message.
func (m ItemReference) Marshal() []byte {
	var b []byte
	b = encMsg(b, 1, m.UID)
	b = encMsg(b, 2, m.DataType)
	b = encU64(b, 3, m.DataSize)
	b = encU32(b, 4, m.RequestedFlags)
	b = encU32(b, 5, m.TransactionID)
	b = encEnum(b, 6, m.Urgency)
	b = encU32(b, 7, m.ModifiedTime)
	return append(b, m.unknown...)
}

func (m *ItemReference) unmarshal(b []byte) error {
	return walk("ItemReference", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decMsg(&m.UID, f)
		case 2:
			return true, decMsg(&m.DataType, f)
		case 3:
			return true, decU64(&m.DataSize, f)
		case 4:
			return true, decU32(&m.RequestedFlags, f)
		case 5:
			return true, decU32(&m.TransactionID, f)
		case 6:
			return true, decEnum(&m.Urgency, f)
		case 7:
			return true, decU32(&m.ModifiedTime, f)
		}
		return false, nil
	})
}

// Capabilities is GDI.Proto.FileAccess.Capabilities. It is not a FileAccess service field: it
// travels in Core FeatureCapabilitiesRequest/Response field 16.
type Capabilities struct {
	ServerPushFileBundleSupport            *bool           // 1
	SoftwareUpdatePartNumberRequestSupport *bool           // 2
	ServerFileChecksumMethod               *ChecksumMethod // 3
	ChecksumMaxFileSizeByte                *uint32         // 4
	CustomFlagSupport                      *bool           // 5

	unknown []byte
}

// ParseCapabilities decodes a Capabilities message.
func ParseCapabilities(b []byte) (Capabilities, error) { return parse[Capabilities](b) }

// Marshal encodes the message.
func (m Capabilities) Marshal() []byte {
	var b []byte
	b = encBool(b, 1, m.ServerPushFileBundleSupport)
	b = encBool(b, 2, m.SoftwareUpdatePartNumberRequestSupport)
	b = encEnum(b, 3, m.ServerFileChecksumMethod)
	b = encU32(b, 4, m.ChecksumMaxFileSizeByte)
	b = encBool(b, 5, m.CustomFlagSupport)
	return append(b, m.unknown...)
}

func (m *Capabilities) unmarshal(b []byte) error {
	return walk("Capabilities", b, &m.unknown, func(f *field) (bool, error) {
		switch f.num {
		case 1:
			return true, decBool(&m.ServerPushFileBundleSupport, f)
		case 2:
			return true, decBool(&m.SoftwareUpdatePartNumberRequestSupport, f)
		case 3:
			return true, decEnum(&m.ServerFileChecksumMethod, f)
		case 4:
			return true, decU32(&m.ChecksumMaxFileSizeByte, f)
		case 5:
			return true, decBool(&m.CustomFlagSupport, f)
		}
		return false, nil
	})
}
