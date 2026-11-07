// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package watch

import (
	"encoding/binary"
	"time"
)

// GarminEpoch is the Unix time of 1989-12-31 00:00 UTC, the zero of Garmin timestamps.
const GarminEpoch = 631065600

// GarminTime converts t to Garmin epoch seconds (0 for times before the epoch).
func GarminTime(t time.Time) uint32 {
	s := t.Unix() - GarminEpoch
	if s < 0 || t.IsZero() {
		return 0
	}
	return uint32(s)
}

// CurrentTimeReply builds the data of the RESPONSE to CURRENT_TIME_REQUEST (docs/protocol/02
// §5.5), after [5052][status]:
//
//	[reference u32][now u32 Garmin epoch, UTC][UTC offset i32 incl. DST][next DST start u32][next DST end u32]
//
// The two transition times follow Garmin Connect, observed in the capture of note 05: in
// November it sent the coming March DST start first, then the October DST end. Zones without
// DST send 0 for both.
func CurrentTimeReply(reference uint32, now time.Time, loc *time.Location) []byte {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	_, offset := local.Zone()
	start, end := nextDSTTransitions(local)
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b[0:], reference)
	binary.LittleEndian.PutUint32(b[4:], GarminTime(now))
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(offset)))
	binary.LittleEndian.PutUint32(b[12:], GarminTime(start))
	binary.LittleEndian.PutUint32(b[16:], GarminTime(end))
	return b
}

// nextDSTTransitions returns the next instant DST begins and the next instant it ends after
// t, looking at most two years ahead. Either is zero if the zone has none.
func nextDSTTransitions(t time.Time) (start, end time.Time) {
	cur := t
	limit := t.AddDate(2, 0, 0)
	for (start.IsZero() || end.IsZero()) && cur.Before(limit) {
		_, next := cur.ZoneBounds()
		if next.IsZero() {
			break
		}
		next = next.In(t.Location())
		if next.IsDST() && !cur.IsDST() && start.IsZero() {
			start = next
		} else if !next.IsDST() && cur.IsDST() && end.IsZero() {
			end = next
		}
		cur = next
	}
	return start, end
}
