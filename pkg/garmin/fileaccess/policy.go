// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import (
	"strconv"
	"strings"
)

// Action is what a sync does with a listed item.
type Action int

const (
	// Ignore leaves the item alone.
	Ignore Action = iota
	// PullOnce stores the item but never flags it as synced, so the watch keeps it.
	PullOnce
	// PullArchive stores the item, then flags it as synced. The watch may delete it after
	// that, which is how health files leave the watch (docs/protocol/05 §7).
	PullArchive
)

func (a Action) String() string {
	switch a {
	case PullOnce:
		return "pull"
	case PullArchive:
		return "pull+archive"
	}
	return "ignore"
}

// Policy chooses the action for an item.
type Policy func(it Item) Action

// FITType returns n for a GDXML type name "FIT_TYPE_<n>".
func FITType(name string) (int, bool) {
	s, ok := strings.CutPrefix(name, "FIT_TYPE_")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// archiveTypes are the health and activity FIT types of the application plan's item policy
// (4 activity, 32 monitoring, 44 metrics, and the other health types the Fenix 8 lists). 72, a
// daily health file with nap events, joined after the live sync showed Garmin Connect flagging
// it (docs/protocol/06, M2).
var archiveTypes = map[int]bool{
	4: true, 9: true, 15: true, 28: true, 32: true, 35: true, 38: true, 41: true, 44: true,
	49: true, 61: true, 68: true, 70: true, 71: true, 72: true, 73: true, 74: true, 77: true,
	79: true,
}

// DefaultPolicy archives the health and activity FIT types, pulls every other FIT type
// (settings, courses, workouts, …) once without archiving, and ignores everything else (crash
// reports, KPI logs, GDXML device files).
func DefaultPolicy(it Item) Action {
	n, ok := FITType(it.TypeName)
	switch {
	case !ok:
		return Ignore
	case archiveTypes[n]:
		return PullArchive
	default:
		return PullOnce
	}
}
