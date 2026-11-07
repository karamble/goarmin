// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MessageID identifies a GFDI message type.
type MessageID uint16

// GFDI message IDs (docs/protocol/02-gfdi-session.md, 03, 04).
const (
	Response                  MessageID = 5000
	DownloadRequest           MessageID = 5002
	UploadRequest             MessageID = 5003
	FileTransferData          MessageID = 5004
	CreateFile                MessageID = 5005
	DeleteFile                MessageID = 5006
	Filter                    MessageID = 5007
	SetFileFlag               MessageID = 5008
	FileAvailable             MessageID = 5009
	FitDefinition             MessageID = 5011
	FitData                   MessageID = 5012
	WeatherRequest            MessageID = 5014
	CancelFileTransfer        MessageID = 5022
	BatteryStatus             MessageID = 5023
	DeviceInformation         MessageID = 5024
	DeviceSettings            MessageID = 5026
	QueuedDownload            MessageID = 5027
	SystemEvent               MessageID = 5030
	SupportedFileTypesRequest MessageID = 5031
	NotificationUpdate        MessageID = 5033
	NotificationControl       MessageID = 5034
	NotificationData          MessageID = 5035
	NotificationSubscription  MessageID = 5036
	Synchronization           MessageID = 5037
	FindMyPhoneRequest        MessageID = 5039
	FindMyPhoneCancel         MessageID = 5040
	MusicControl              MessageID = 5041
	MusicControlCapabilities  MessageID = 5042
	ProtobufRequest           MessageID = 5043
	ProtobufResponse          MessageID = 5044
	CancelProtobuf            MessageID = 5045
	MusicControlEntityUpdate  MessageID = 5049
	Configuration             MessageID = 5050
	CurrentTimeRequest        MessageID = 5052
	CompressedFileData        MessageID = 5054
	AuthNegotiation           MessageID = 5101
	compactBase               MessageID = 5000
	compactFlag               uint16    = 0x8000
)

var messageNames = map[MessageID]string{
	Response: "RESPONSE", DownloadRequest: "DOWNLOAD_REQUEST", UploadRequest: "UPLOAD_REQUEST",
	FileTransferData: "FILE_TRANSFER_DATA", CreateFile: "CREATE_FILE", DeleteFile: "DELETE_FILE",
	Filter: "FILTER", SetFileFlag: "SET_FILE_FLAG", FileAvailable: "FILE_AVAILABLE",
	FitDefinition: "FIT_DEFINITION", FitData: "FIT_DATA", WeatherRequest: "WEATHER_REQUEST",
	CancelFileTransfer: "CANCEL_FILE_TRANSFER", BatteryStatus: "BATTERY_STATUS",
	DeviceInformation: "DEVICE_INFORMATION", DeviceSettings: "DEVICE_SETTINGS",
	QueuedDownload: "QUEUED_DOWNLOAD", SystemEvent: "SYSTEM_EVENT",
	SupportedFileTypesRequest: "SUPPORTED_FILE_TYPES_REQUEST", NotificationUpdate: "NOTIFICATION_UPDATE",
	NotificationControl: "NOTIFICATION_CONTROL", NotificationData: "NOTIFICATION_DATA",
	NotificationSubscription: "NOTIFICATION_SUBSCRIPTION", Synchronization: "SYNCHRONIZATION",
	FindMyPhoneRequest: "FIND_MY_PHONE_REQUEST", FindMyPhoneCancel: "FIND_MY_PHONE_CANCEL",
	MusicControl: "MUSIC_CONTROL", MusicControlCapabilities: "MUSIC_CONTROL_CAPABILITIES",
	ProtobufRequest: "PROTOBUF_REQUEST", ProtobufResponse: "PROTOBUF_RESPONSE",
	CancelProtobuf: "CANCEL_PROTOBUF", MusicControlEntityUpdate: "MUSIC_CONTROL_ENTITY_UPDATE",
	Configuration: "CONFIGURATION", CurrentTimeRequest: "CURRENT_TIME_REQUEST",
	CompressedFileData: "COMPRESSED_FILE_DATA", AuthNegotiation: "AUTH_NEGOTIATION",
}

func (id MessageID) String() string {
	if n, ok := messageNames[id]; ok {
		return n
	}
	return fmt.Sprintf("MESSAGE_%d", uint16(id))
}

// Frame is one GFDI message:
//
//	[length u16 LE][message ID u16 LE][payload][CRC-16 u16 LE]
//
// where length counts the whole frame and the CRC covers everything before it. In compact
// ("transaction") form, bit 15 of the ID field is set, its low byte is ID − 5000 and bits 8..14
// carry a sequence number. Garmin Connect and the Fenix 8 use compact frames in both
// directions, and a response repeats the sequence number of the message it answers.
type Frame struct {
	ID      MessageID
	Compact bool
	// Seq is the transaction sequence number of a compact frame (7 bits).
	Seq     uint8
	Payload []byte
}

// Errors returned by ParseFrame.
var (
	ErrShortFrame = errors.New("gfdi: frame shorter than 6 bytes")
	ErrLength     = errors.New("gfdi: length field does not match the frame")
	ErrCRC        = errors.New("gfdi: CRC mismatch")
)

// ParseFrame decodes and checks one GFDI frame (as delivered by the COBS layer).
func ParseFrame(b []byte) (Frame, error) {
	if len(b) < 6 {
		return Frame{}, ErrShortFrame
	}
	if int(binary.LittleEndian.Uint16(b)) != len(b) {
		return Frame{}, fmt.Errorf("%w: field %d, frame %d bytes", ErrLength, binary.LittleEndian.Uint16(b), len(b))
	}
	if want, got := binary.LittleEndian.Uint16(b[len(b)-2:]), CRC16(0, b[:len(b)-2]); want != got {
		return Frame{}, fmt.Errorf("%w: frame says 0x%04X, computed 0x%04X", ErrCRC, want, got)
	}
	raw := binary.LittleEndian.Uint16(b[2:4])
	f := Frame{ID: MessageID(raw), Payload: append([]byte(nil), b[4:len(b)-2]...)}
	if raw&compactFlag != 0 {
		f.Compact = true
		f.ID = compactBase + MessageID(raw&0xFF)
		f.Seq = uint8(raw>>8) & 0x7F
	}
	return f, nil
}

// Marshal encodes the frame with its length and CRC.
func (f Frame) Marshal() []byte {
	n := 4 + len(f.Payload) + 2
	b := make([]byte, n)
	binary.LittleEndian.PutUint16(b[0:2], uint16(n))
	raw := uint16(f.ID)
	if f.Compact {
		raw = compactFlag | uint16(f.Seq&0x7F)<<8 | uint16(f.ID-compactBase)&0xFF
	}
	binary.LittleEndian.PutUint16(b[2:4], raw)
	copy(b[4:], f.Payload)
	binary.LittleEndian.PutUint16(b[n-2:], CRC16(0, b[:n-2]))
	return b
}

func (f Frame) String() string {
	s := f.ID.String()
	if f.Compact {
		s += fmt.Sprintf(" [seq %d]", f.Seq)
	}
	return fmt.Sprintf("%s (%d bytes)", s, len(f.Payload))
}

// Status is the status byte of a RESPONSE.
type Status byte

// Response statuses.
const (
	StatusACK         Status = 0
	StatusNAK         Status = 1
	StatusUnsupported Status = 2
	StatusDecodeError Status = 3
	StatusCRCError    Status = 4
	StatusLengthError Status = 5
)

// ResponseMessage is a decoded RESPONSE (5000) payload.
type ResponseMessage struct {
	Original MessageID
	Status   Status
	// Data holds the message-specific bytes after the status.
	Data []byte
}

// ParseResponse decodes a RESPONSE payload.
func ParseResponse(payload []byte) (ResponseMessage, error) {
	if len(payload) < 3 {
		return ResponseMessage{}, fmt.Errorf("gfdi: %d-byte RESPONSE payload", len(payload))
	}
	return ResponseMessage{
		Original: MessageID(binary.LittleEndian.Uint16(payload)),
		Status:   Status(payload[2]),
		Data:     append([]byte(nil), payload[3:]...),
	}, nil
}

// ReplyTo builds a RESPONSE to req, in the same frame form: compact with req's sequence
// number when req was compact.
func ReplyTo(req Frame, status Status, data []byte) Frame {
	p := make([]byte, 3, 3+len(data))
	binary.LittleEndian.PutUint16(p, uint16(req.ID))
	p[2] = byte(status)
	return Frame{ID: Response, Compact: req.Compact, Seq: req.Seq, Payload: append(p, data...)}
}
