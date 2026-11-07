// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package realtime streams live values from a Garmin watch: heart rate, steps, stress, Body
// Battery, respiration, SpO2 and beat-to-beat intervals. Each value has its own Multi-Link
// service, registered without MLR; every notification on it carries one service message
// (docs/protocol/01 §2.8). The watch measures continuously anyway; streaming only costs radio
// time while the link is up.
package realtime

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// Reading is one live value.
type Reading struct {
	Service multilink.Service
	At      time.Time // when it arrived (the watch's own time for SpO2)
	// heart_rate, resting_hr, steps, step_goal, stress, body_battery, respiration, spo2,
	// rr_interval, calories, active_calories, floors_up, floors_down, floors_goal,
	// intensity_goal, raw
	Name  string
	Value float64
	Unit  string
	Raw   []byte // the service message as received
}

// Services maps the names the command line and the API use to services.
var Services = map[string]multilink.Service{
	"hr":           multilink.ServiceRealtimeHR,
	"steps":        multilink.ServiceRealtimeSteps,
	"stress":       multilink.ServiceRealtimeStress,
	"body_battery": multilink.ServiceRealtimeBodyBattery,
	"respiration":  multilink.ServiceRealtimeRespiration,
	"spo2":         multilink.ServiceRealtimeSpO2,
	"rr":           multilink.ServiceRealtimeHRV,
	"calories":     multilink.ServiceRealtimeCalories,
	"floors":       multilink.ServiceRealtimeFloors,
	"intensity":    multilink.ServiceRealtimeIntensity,
	"active_time":  multilink.ServiceRealtimeActiveTime,
}

// Decode turns one service message into readings. Messages it cannot interpret come back as a
// single "raw" reading, so new formats show up instead of vanishing.
func Decode(svc multilink.Service, p []byte, now time.Time) []Reading {
	r := func(name string, v float64, unit string) Reading {
		return Reading{Service: svc, At: now, Name: name, Value: v, Unit: unit, Raw: p}
	}
	switch svc {
	case multilink.ServiceRealtimeHR:
		// [type][hr][resting] ff ff (Gadgetbridge: type 0/2/3).
		if len(p) < 3 {
			break
		}
		var out []Reading
		if p[1] != 0 && p[1] != 0xFF {
			out = append(out, r("heart_rate", float64(p[1]), "bpm"))
		}
		if p[2] != 0 && p[2] != 0xFF {
			out = append(out, r("resting_hr", float64(p[2]), "bpm"))
		}
		if len(out) > 0 {
			return out
		}
		return nil // no reading yet (sensor warming up, off the wrist)
	case multilink.ServiceRealtimeSteps:
		if len(p) >= 8 {
			return []Reading{
				r("steps", float64(binary.LittleEndian.Uint32(p)), "steps"),
				r("step_goal", float64(binary.LittleEndian.Uint32(p[4:])), "steps"),
			}
		}
	case multilink.ServiceRealtimeBodyBattery:
		if len(p) >= 1 {
			if v := int8(p[0]); v >= 0 && v <= 100 {
				return []Reading{r("body_battery", float64(v), "%")}
			}
			return nil // 0x7F: unknown
		}
	case multilink.ServiceRealtimeRespiration:
		if len(p) >= 1 {
			if v := int8(p[0]); v > 0 {
				return []Reading{r("respiration", float64(v), "brpm")}
			}
			return nil // negative (usually -2): unknown
		}
	case multilink.ServiceRealtimeSpO2:
		if len(p) >= 5 {
			v := int8(p[0])
			if v <= 0 {
				return nil
			}
			x := r("spo2", float64(v), "%")
			x.At = time.Unix(int64(binary.LittleEndian.Uint32(p[1:]))+631065600, 0)
			return []Reading{x}
		}
	case multilink.ServiceRealtimeStress:
		// [stress u16 LE][i16]: on the Fenix 8 e.g. 16 00 fc 00 = 22; the second value (-4, -2) is
		// not understood yet. Gadgetbridge does not decode this service.
		if len(p) >= 2 {
			if v := binary.LittleEndian.Uint16(p); v <= 100 {
				return []Reading{r("stress", float64(v), "")}
			}
			return nil
		}
	case multilink.ServiceRealtimeHRV:
		if len(p) >= 2 {
			return []Reading{r("rr_interval", float64(binary.LittleEndian.Uint16(p)), "ms")}
		}
	case multilink.ServiceRealtimeCalories:
		// [total u32][active u32] kcal today (Gadgetbridge RealtimeCaloriesCallback); the Fenix 8
		// sent 33 09 00 00 b0 00 00 00 = 2355 total, 176 active.
		if len(p) >= 8 {
			return []Reading{
				r("calories", float64(binary.LittleEndian.Uint32(p)), "kcal"),
				r("active_calories", float64(binary.LittleEndian.Uint32(p[4:])), "kcal"),
			}
		}
	case multilink.ServiceRealtimeFloors:
		// [up u16][down u16][goal u16] floors today (Gadgetbridge RealtimeFloorsCallback).
		if len(p) >= 6 {
			return []Reading{
				r("floors_up", float64(binary.LittleEndian.Uint16(p)), "floors"),
				r("floors_down", float64(binary.LittleEndian.Uint16(p[2:])), "floors"),
				r("floors_goal", float64(binary.LittleEndian.Uint16(p[4:])), "floors"),
			}
		}
	case multilink.ServiceRealtimeIntensity:
		// 14 bytes; the Fenix 8 sent ten zero bytes (a week without intensity minutes) and then
		// 96 00 00 00 = 150, the weekly goal. The rest is not understood yet, so the message is
		// also passed on raw.
		if len(p) >= 14 {
			return []Reading{r("intensity_goal", float64(binary.LittleEndian.Uint32(p[10:])), "min"), r("raw", 0, "")}
		}
	}
	return []Reading{r("raw", 0, "")}
}

// Stream is a set of open realtime services.
type Stream struct {
	out  chan Reading
	now  func() time.Time
	mu   sync.Mutex
	open map[multilink.Service]*multilink.Channel
	wg   sync.WaitGroup
}

// Open registers the services on mux and starts streaming. Services the watch refuses are
// skipped; Open fails only if none could be opened.
func Open(ctx context.Context, mux *multilink.Mux, services []multilink.Service) (*Stream, error) {
	s := &Stream{out: make(chan Reading, 64), now: time.Now, open: map[multilink.Service]*multilink.Channel{}}
	var errs []error
	for _, svc := range services {
		ch, err := mux.Register(ctx, svc, false)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", svc, err))
			continue
		}
		s.open[svc] = ch
	}
	if len(s.open) == 0 {
		return nil, fmt.Errorf("realtime: no service opened: %w", errors.Join(errs...))
	}
	for svc, ch := range s.open {
		s.wg.Add(1)
		go s.read(svc, ch)
	}
	go func() {
		s.wg.Wait()
		close(s.out)
	}()
	return s, nil
}

// Opened lists the services that are streaming.
func (s *Stream) Opened() []multilink.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]multilink.Service, 0, len(s.open))
	for svc := range s.open {
		out = append(out, svc)
	}
	return out
}

func (s *Stream) read(svc multilink.Service, ch *multilink.Channel) {
	defer s.wg.Done()
	for {
		select {
		case p, ok := <-ch.Recv():
			if !ok {
				return
			}
			for _, r := range Decode(svc, p, s.now()) {
				select {
				case s.out <- r:
				default: // a slow reader loses readings, never the link
				}
			}
		case <-ch.Closed():
			return
		}
	}
}

// Readings delivers the values; it is closed when every service has closed.
func (s *Stream) Readings() <-chan Reading { return s.out }

// Close closes every service on the watch.
func (s *Stream) Close(ctx context.Context) error {
	s.mu.Lock()
	chans := s.open
	s.mu.Unlock()
	var errs []error
	for _, ch := range chans {
		if err := ch.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
