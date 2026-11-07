// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// One case per exercise category the FIT profile names exercises for: each maps the category
// to its typedef.<Category>ExerciseName (muktihari/fit v0.28.5). A new profile version may add
// categories; they then read as "" until a case is added.

package fitwatch

import "github.com/muktihari/fit/profile/typedef"

// exerciseEnum is the FIT profile's name for exercise number name in category ("cat_cow"), or
// the enum's invalid form for numbers it does not know.
func exerciseEnum(category typedef.ExerciseCategory, name uint16) string {
	switch category {
	case typedef.ExerciseCategoryBandedExercises:
		return typedef.BandedExercisesExerciseName(name).String()
	case typedef.ExerciseCategoryBattleRope:
		return typedef.BattleRopeExerciseName(name).String()
	case typedef.ExerciseCategoryBenchPress:
		return typedef.BenchPressExerciseName(name).String()
	case typedef.ExerciseCategoryBike:
		return typedef.BikeExerciseName(name).String()
	case typedef.ExerciseCategoryBikeOutdoor:
		return typedef.BikeOutdoorExerciseName(name).String()
	case typedef.ExerciseCategoryCalfRaise:
		return typedef.CalfRaiseExerciseName(name).String()
	case typedef.ExerciseCategoryCardio:
		return typedef.CardioExerciseName(name).String()
	case typedef.ExerciseCategoryCarry:
		return typedef.CarryExerciseName(name).String()
	case typedef.ExerciseCategoryChop:
		return typedef.ChopExerciseName(name).String()
	case typedef.ExerciseCategoryCore:
		return typedef.CoreExerciseName(name).String()
	case typedef.ExerciseCategoryCrunch:
		return typedef.CrunchExerciseName(name).String()
	case typedef.ExerciseCategoryCurl:
		return typedef.CurlExerciseName(name).String()
	case typedef.ExerciseCategoryDeadlift:
		return typedef.DeadliftExerciseName(name).String()
	case typedef.ExerciseCategoryElliptical:
		return typedef.EllipticalExerciseName(name).String()
	case typedef.ExerciseCategoryFloorClimb:
		return typedef.FloorClimbExerciseName(name).String()
	case typedef.ExerciseCategoryFlye:
		return typedef.FlyeExerciseName(name).String()
	case typedef.ExerciseCategoryHipRaise:
		return typedef.HipRaiseExerciseName(name).String()
	case typedef.ExerciseCategoryHipStability:
		return typedef.HipStabilityExerciseName(name).String()
	case typedef.ExerciseCategoryHipSwing:
		return typedef.HipSwingExerciseName(name).String()
	case typedef.ExerciseCategoryHyperextension:
		return typedef.HyperextensionExerciseName(name).String()
	case typedef.ExerciseCategoryIndoorBike:
		return typedef.IndoorBikeExerciseName(name).String()
	case typedef.ExerciseCategoryIndoorRow:
		return typedef.IndoorRowExerciseName(name).String()
	case typedef.ExerciseCategoryLadder:
		return typedef.LadderExerciseName(name).String()
	case typedef.ExerciseCategoryLateralRaise:
		return typedef.LateralRaiseExerciseName(name).String()
	case typedef.ExerciseCategoryLegCurl:
		return typedef.LegCurlExerciseName(name).String()
	case typedef.ExerciseCategoryLegRaise:
		return typedef.LegRaiseExerciseName(name).String()
	case typedef.ExerciseCategoryLunge:
		return typedef.LungeExerciseName(name).String()
	case typedef.ExerciseCategoryMove:
		return typedef.MoveExerciseName(name).String()
	case typedef.ExerciseCategoryOlympicLift:
		return typedef.OlympicLiftExerciseName(name).String()
	case typedef.ExerciseCategoryPlank:
		return typedef.PlankExerciseName(name).String()
	case typedef.ExerciseCategoryPlyo:
		return typedef.PlyoExerciseName(name).String()
	case typedef.ExerciseCategoryPose:
		return typedef.PoseExerciseName(name).String()
	case typedef.ExerciseCategoryPullUp:
		return typedef.PullUpExerciseName(name).String()
	case typedef.ExerciseCategoryPushUp:
		return typedef.PushUpExerciseName(name).String()
	case typedef.ExerciseCategoryRow:
		return typedef.RowExerciseName(name).String()
	case typedef.ExerciseCategoryRun:
		return typedef.RunExerciseName(name).String()
	case typedef.ExerciseCategoryRunIndoor:
		return typedef.RunIndoorExerciseName(name).String()
	case typedef.ExerciseCategorySandbag:
		return typedef.SandbagExerciseName(name).String()
	case typedef.ExerciseCategoryShoulderPress:
		return typedef.ShoulderPressExerciseName(name).String()
	case typedef.ExerciseCategoryShoulderStability:
		return typedef.ShoulderStabilityExerciseName(name).String()
	case typedef.ExerciseCategoryShrug:
		return typedef.ShrugExerciseName(name).String()
	case typedef.ExerciseCategorySitUp:
		return typedef.SitUpExerciseName(name).String()
	case typedef.ExerciseCategorySled:
		return typedef.SledExerciseName(name).String()
	case typedef.ExerciseCategorySledgeHammer:
		return typedef.SledgeHammerExerciseName(name).String()
	case typedef.ExerciseCategorySquat:
		return typedef.SquatExerciseName(name).String()
	case typedef.ExerciseCategoryStairStepper:
		return typedef.StairStepperExerciseName(name).String()
	case typedef.ExerciseCategorySuspension:
		return typedef.SuspensionExerciseName(name).String()
	case typedef.ExerciseCategoryTire:
		return typedef.TireExerciseName(name).String()
	case typedef.ExerciseCategoryTotalBody:
		return typedef.TotalBodyExerciseName(name).String()
	case typedef.ExerciseCategoryTricepsExtension:
		return typedef.TricepsExtensionExerciseName(name).String()
	case typedef.ExerciseCategoryWarmUp:
		return typedef.WarmUpExerciseName(name).String()
	}
	return ""
}
