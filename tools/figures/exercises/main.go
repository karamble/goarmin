// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command exercises prints every exercise of Garmin's published FIT profile (muktihari/fit's
// typedef, from the official SDK) as JSON: category, its number, the exercise's number and name.
// It is the work list of the exercise figures (tools/figures): one pose per entry.
//
//	go run ./tools/figures/exercises > tools/figures/exercises.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/muktihari/fit/profile/typedef"
)

// Exercise is one exercise of the profile.
type Exercise struct {
	Category    string `json:"category"`
	CategoryNum uint16 `json:"category_num"`
	Number      uint16 `json:"number"`
	Name        string `json:"name"`
}

type named interface {
	~uint16
	String() string
}

// names lists an exercise-name enumeration without its invalid value.
func names[T named](vs []T) []Exercise {
	var out []Exercise
	for _, v := range vs {
		if s := v.String(); !strings.Contains(s, "Invalid") {
			out = append(out, Exercise{Number: uint16(v), Name: s})
		}
	}
	return out
}

// One row per category that has an exercise-name enumeration (as in pkg/fitwatch/exercise.go).
var categories = []struct {
	cat   typedef.ExerciseCategory
	names []Exercise
}{
	{typedef.ExerciseCategoryBandedExercises, names(typedef.ListBandedExercisesExerciseName())},
	{typedef.ExerciseCategoryBattleRope, names(typedef.ListBattleRopeExerciseName())},
	{typedef.ExerciseCategoryBenchPress, names(typedef.ListBenchPressExerciseName())},
	{typedef.ExerciseCategoryBike, names(typedef.ListBikeExerciseName())},
	{typedef.ExerciseCategoryBikeOutdoor, names(typedef.ListBikeOutdoorExerciseName())},
	{typedef.ExerciseCategoryCalfRaise, names(typedef.ListCalfRaiseExerciseName())},
	{typedef.ExerciseCategoryCardio, names(typedef.ListCardioExerciseName())},
	{typedef.ExerciseCategoryCarry, names(typedef.ListCarryExerciseName())},
	{typedef.ExerciseCategoryChop, names(typedef.ListChopExerciseName())},
	{typedef.ExerciseCategoryCore, names(typedef.ListCoreExerciseName())},
	{typedef.ExerciseCategoryCrunch, names(typedef.ListCrunchExerciseName())},
	{typedef.ExerciseCategoryCurl, names(typedef.ListCurlExerciseName())},
	{typedef.ExerciseCategoryDeadlift, names(typedef.ListDeadliftExerciseName())},
	{typedef.ExerciseCategoryElliptical, names(typedef.ListEllipticalExerciseName())},
	{typedef.ExerciseCategoryFloorClimb, names(typedef.ListFloorClimbExerciseName())},
	{typedef.ExerciseCategoryFlye, names(typedef.ListFlyeExerciseName())},
	{typedef.ExerciseCategoryHipRaise, names(typedef.ListHipRaiseExerciseName())},
	{typedef.ExerciseCategoryHipStability, names(typedef.ListHipStabilityExerciseName())},
	{typedef.ExerciseCategoryHipSwing, names(typedef.ListHipSwingExerciseName())},
	{typedef.ExerciseCategoryHyperextension, names(typedef.ListHyperextensionExerciseName())},
	{typedef.ExerciseCategoryIndoorBike, names(typedef.ListIndoorBikeExerciseName())},
	{typedef.ExerciseCategoryIndoorRow, names(typedef.ListIndoorRowExerciseName())},
	{typedef.ExerciseCategoryLadder, names(typedef.ListLadderExerciseName())},
	{typedef.ExerciseCategoryLateralRaise, names(typedef.ListLateralRaiseExerciseName())},
	{typedef.ExerciseCategoryLegCurl, names(typedef.ListLegCurlExerciseName())},
	{typedef.ExerciseCategoryLegRaise, names(typedef.ListLegRaiseExerciseName())},
	{typedef.ExerciseCategoryLunge, names(typedef.ListLungeExerciseName())},
	{typedef.ExerciseCategoryMove, names(typedef.ListMoveExerciseName())},
	{typedef.ExerciseCategoryOlympicLift, names(typedef.ListOlympicLiftExerciseName())},
	{typedef.ExerciseCategoryPlank, names(typedef.ListPlankExerciseName())},
	{typedef.ExerciseCategoryPlyo, names(typedef.ListPlyoExerciseName())},
	{typedef.ExerciseCategoryPose, names(typedef.ListPoseExerciseName())},
	{typedef.ExerciseCategoryPullUp, names(typedef.ListPullUpExerciseName())},
	{typedef.ExerciseCategoryPushUp, names(typedef.ListPushUpExerciseName())},
	{typedef.ExerciseCategoryRow, names(typedef.ListRowExerciseName())},
	{typedef.ExerciseCategoryRun, names(typedef.ListRunExerciseName())},
	{typedef.ExerciseCategoryRunIndoor, names(typedef.ListRunIndoorExerciseName())},
	{typedef.ExerciseCategorySandbag, names(typedef.ListSandbagExerciseName())},
	{typedef.ExerciseCategoryShoulderPress, names(typedef.ListShoulderPressExerciseName())},
	{typedef.ExerciseCategoryShoulderStability, names(typedef.ListShoulderStabilityExerciseName())},
	{typedef.ExerciseCategoryShrug, names(typedef.ListShrugExerciseName())},
	{typedef.ExerciseCategorySitUp, names(typedef.ListSitUpExerciseName())},
	{typedef.ExerciseCategorySled, names(typedef.ListSledExerciseName())},
	{typedef.ExerciseCategorySledgeHammer, names(typedef.ListSledgeHammerExerciseName())},
	{typedef.ExerciseCategorySquat, names(typedef.ListSquatExerciseName())},
	{typedef.ExerciseCategoryStairStepper, names(typedef.ListStairStepperExerciseName())},
	{typedef.ExerciseCategorySuspension, names(typedef.ListSuspensionExerciseName())},
	{typedef.ExerciseCategoryTire, names(typedef.ListTireExerciseName())},
	{typedef.ExerciseCategoryTotalBody, names(typedef.ListTotalBodyExerciseName())},
	{typedef.ExerciseCategoryTricepsExtension, names(typedef.ListTricepsExtensionExerciseName())},
	{typedef.ExerciseCategoryWarmUp, names(typedef.ListWarmUpExerciseName())},
}

func main() {
	var all []Exercise
	for _, c := range categories {
		for _, e := range c.names {
			e.Category, e.CategoryNum = c.cat.String(), uint16(c.cat)
			all = append(all, e)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(all); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%d exercises in %d categories\n", len(all), len(categories))
}
