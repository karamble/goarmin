// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"bytes"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// Smart service field numbers (garmin_vivomovehr.Smart). Each Smart message carries one of
// these services as a length-delimited field. Field 51 is not defined.
const (
	ServiceCalendar               = 1
	ServiceConnectIQHTTP          = 2
	ServiceConnectIQInstalledApps = 3
	ServiceConnectIQAppSettings   = 4
	ServiceInternationalGolf      = 5
	ServiceSwingSensor            = 6
	ServiceDataTransfer           = 7
	ServiceDeviceStatus           = 8
	ServiceIncidentDetection      = 9
	ServiceAudioPrompts           = 10
	ServiceWifiSetup              = 11
	ServiceFindMyWatch            = 12
	ServiceCore                   = 13
	ServiceGroupLiveTrack         = 14
	ServiceExpresspayCommand      = 15
	ServiceSMSNotification        = 16
	ServiceLiveTrackMessaging     = 17
	ServiceInstantInput           = 18
	ServiceSportProfileSetup      = 19
	ServiceHSAData                = 20
	ServiceLiveTrack              = 21
	ServiceExploreSync            = 22
	ServiceWaypointTransfer       = 23
	ServiceDeviceMessage          = 24
	ServiceLTE                    = 25
	ServiceAntiTheftAlarm         = 26
	ServiceCredentials            = 27
	ServiceInReachTracking        = 28
	ServiceInReachMessaging       = 29
	ServiceEventSharing           = 30
	ServiceGenericItemTransfer    = 31
	ServiceInReachContactSync     = 32
	ServiceHandCalibration        = 33
	ServiceInReachWeather         = 34
	ServiceWind                   = 35
	ServiceSimpleSetup            = 36
	ServiceSwitcher               = 37
	ServiceLaunchMonitor          = 38
	ServiceECG                    = 39 // samd_service
	ServiceNavigationSync         = 40
	ServiceKidDevice              = 41
	ServiceRealtimeSettings       = 42
	ServiceFileAccess             = 43
	Service44                     = 44
	ServiceDezl                   = 45
	ServiceDeepLinking            = 46
	ServiceGarminConnect          = 47
	ServiceXeroChronograph        = 48
	ServiceGNCS                   = 49
	ServiceInReachConfiguration   = 50
	ServiceDive                   = 52
	ServiceMessengerOnDevice      = 53
	ServiceCompanionRangefinder   = 54
	ServiceDog                    = 55
	ServiceLiveSession            = 56
	ServiceRadar                  = 57
	ServiceTopsideBuoy            = 58
	ServiceIopMessage             = 59
	ServiceStoreAndForward        = 60
	ServiceHeartRate              = 61
	ServiceRunning                = 62
	ServiceAccessoryUtilities     = 63
	ServiceBikePower              = 64
	ServiceDevelopmentTesting     = 65
	ServiceXeroGameCam            = 66
	ServiceConnectionManagement   = 67
	ServiceInReachCalls           = 68
	ServiceLiveSync               = 69
	ServicePSASample              = 70
	ServiceSubscription           = 71
)

var serviceNames = map[int]string{
	1: "calendar_events_service", 2: "connect_iq_http_service",
	3: "connect_iq_installed_apps_service", 4: "connect_iq_app_settings_service",
	5: "international_golf_service", 6: "swing_sensor_service", 7: "data_transfer_service",
	8: "device_status_service", 9: "incident_detection_service", 10: "audio_prompts_service",
	11: "wifi_setup_service", 12: "find_my_watch_service", 13: "core_service",
	14: "group_live_track_service", 15: "expresspay_command_service",
	16: "sms_notification_service", 17: "live_track_messaging_service",
	18: "instant_input_service", 19: "sport_profile_setup_service", 20: "hsa_data_service",
	21: "live_track_service", 22: "explore_sync_service", 23: "way_point_transfer_service",
	24: "device_message_service", 25: "lte_service", 26: "anti_theft_alarm_service",
	27: "credentials_service", 28: "inreach_tracking_service", 29: "inreach_messaging_service",
	30: "event_sharing", 31: "generic_item_transfer_service", 32: "inreach_contact_sync_service",
	33: "hand_calibration_service", 34: "inreach_weather_service", 35: "wind_service",
	36: "simple_setup_service", 37: "switcher_service", 38: "launch_monitor_service",
	39: "samd_service", 40: "navigation_sync_service", 41: "kid_device_service",
	42: "realtime_settings_service", 43: "file_access_service", 44: "service44",
	45: "dezl_service", 46: "deeplinking_service", 47: "garmin_connect_service",
	48: "xero_chronograph_service", 49: "gncs_service", 50: "inreach_configuration_service",
	52: "dive_service", 53: "messenger_on_device_service", 54: "companion_rangefinder_service",
	55: "dog_service", 56: "live_session_service", 57: "radar_service",
	58: "topside_buoy_service", 59: "iop_message_service", 60: "store_and_forward_service",
	61: "heart_rate_service", 62: "running_service", 63: "accessory_utilities_service",
	64: "bike_power_service", 65: "development_testing_service", 66: "xero_game_cam_service",
	67: "connection_management_service", 68: "inreach_calls_service", 69: "live_sync_service",
	70: "psa_sample_service", 71: "subscription_service",
}

// ServiceName returns the Smart field name of a service number, e.g. "file_access_service",
// or "smart_field_<n>" for a number the schema does not define.
func ServiceName(service int) string {
	if n, ok := serviceNames[service]; ok {
		return n
	}
	return fmt.Sprintf("smart_field_%d", service)
}

// Envelope is one decoded Smart message: the service field number and the service message
// bytes.
type Envelope struct {
	Service int    // Smart field number, one of the Service constants
	Payload []byte // the service message, e.g. a marshalled FileAccess
}

// ParseEnvelope decodes a complete Smart message. It must be exactly one length-delimited
// top-level field; anything else is an error wrapping ErrEnvelope (or io.ErrUnexpectedEOF for
// truncated input). Payload is a copy, so b may be reused.
func ParseEnvelope(b []byte) (Envelope, error) {
	if len(b) == 0 {
		return Envelope{}, fmt.Errorf("smart: envelope: %w: empty message", ErrEnvelope)
	}
	num, typ, n := protowire.ConsumeTag(b)
	if n < 0 {
		return Envelope{}, fmt.Errorf("smart: envelope: %w", protowire.ParseError(n))
	}
	if !num.IsValid() {
		return Envelope{}, fmt.Errorf("smart: envelope: %w: field number %d out of range", ErrEnvelope, num)
	}
	if typ != protowire.BytesType {
		return Envelope{}, fmt.Errorf("smart: envelope field %d: %w: %w: %s, want %s", num, ErrEnvelope,
			ErrWireType, wireTypeName(typ), wireTypeName(protowire.BytesType))
	}
	v, m := protowire.ConsumeBytes(b[n:])
	if m < 0 {
		return Envelope{}, fmt.Errorf("smart: envelope field %d: %w", num, protowire.ParseError(m))
	}
	if rest := len(b) - n - m; rest != 0 {
		return Envelope{}, fmt.Errorf("smart: envelope: %w: %d bytes follow service field %d", ErrEnvelope, rest, num)
	}
	return Envelope{Service: int(num), Payload: bytes.Clone(v)}, nil
}

// Marshal encodes the envelope as a Smart message. Service must be a valid field number
// (1 to 2^29-1); otherwise the result is not a valid Smart message.
func (e Envelope) Marshal() []byte {
	return encLen(nil, protowire.Number(e.Service), e.Payload)
}

// FileAccess decodes the payload of a ServiceFileAccess envelope.
func (e Envelope) FileAccess() (FileAccess, error) {
	if e.Service != ServiceFileAccess {
		return FileAccess{}, fmt.Errorf("smart: envelope carries %s, not file_access_service", ServiceName(e.Service))
	}
	return ParseFileAccess(e.Payload)
}
