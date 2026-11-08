// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package product

import "strings"

// Kind is what sort of device a product is.
type Kind string

// The kinds. A kind is stored, so the strings never change.
const (
	Watch        Kind = "watch"
	Scale        Kind = "scale"
	BikeComputer Kind = "bike_computer"
	Sensor       Kind = "sensor"   // heart-rate straps, pods, radars, power meters, trainers, …
	Handheld     Kind = "handheld" // outdoor GPS units, satellite communicators, cameras
	App          Kind = "app"      // files made by Garmin Connect or another program
	Other        Kind = "other"    // a product neither table names, or none of the above
)

// Words is the kind in words, for people ("bike computer").
func (k Kind) Words() string {
	switch k {
	case BikeComputer:
		return "bike computer"
	case Watch, Scale, Sensor, Handheld, App:
		return string(k)
	}
	return "device"
}

// families map the start of a product's model name (lower case) to its kind. Longer starts
// come first where they overlap: "approach z" (a rangefinder) before "approach" (a watch).
var families = []struct {
	start []string
	kind  Kind
}{
	{[]string{"index"}, Scale},
	{[]string{"edge"}, BikeComputer},
	{[]string{"approach z", "approach r", "approach g", "approach ct", "descent t", "descent s", "hrm", "varia", "vector", "rally",
		"tempe", "speed", "cadence", "sdm", "running dynamics", "ohr", "tacx", "xero", "truswing", "chirp", "bsm", "bcm"}, Sensor},
	{[]string{"gpsmap", "oregon", "etrex", "montana", "rino", "foretrex", "inreach", "astro", "alpha", "virb", "dakota", "colorado",
		"monterra", "overlander", "zumo", "dezl", "drive", "nuvi", "dash"}, Handheld},
	{[]string{"connect", "android", "training center", "garmin connect", "express", "basecamp"}, App},
	{[]string{"forerunner", "fenix", "epix", "enduro", "instinct", "venu", "vivo", "marq", "descent", "approach", "d2 ", "d2",
		"legacy", "tactix", "quatix", "lily", "swim", "bounce", "cirqa"}, Watch},
}

// KindOf is the kind of a Garmin product, from its model name; Other for a product neither
// table names.
func KindOf(product uint16) Kind {
	if product == 0xFFFE {
		return App // what Garmin Connect writes as the product of the files it makes
	}
	name := strings.ToLower(Name(product))
	for _, f := range families {
		for _, s := range f.start {
			if strings.HasPrefix(name, s) {
				return f.kind
			}
		}
	}
	return Other
}
