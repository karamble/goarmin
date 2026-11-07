// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package product names Garmin products for people: 4536 is "Fenix 8", 4315 "Forerunner 965".
// The names come from the FIT profile's product table, tidied up.
package product

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/muktihari/fit/profile/typedef"
)

// regional variants carry the same model name.
var regions = []string{"_japan", "_korea", "_taiwan", "_china", "_asia", "_apac", "_sea", "_india", "_chinese", "_jpn", "_kor", "_twn"}

var words = map[string]string{"fr": "Forerunner", "gen": "Gen", "xt": "XT", "hrm": "HRM", "gps": "GPS"}

// Name is a product number as its model name, or "Garmin <number>" for one the table lacks.
func Name(product uint16) string {
	raw := typedef.GarminProduct(product).String()
	if raw == "" || strings.ContainsAny(raw, "() ") || strings.HasPrefix(raw, "GarminProduct") {
		return "Garmin " + strconv.Itoa(int(product))
	}
	for _, r := range regions {
		raw = strings.TrimSuffix(raw, r)
	}
	var out []string
	for _, part := range strings.Split(raw, "_") {
		out = append(out, splitDigits(part)...)
	}
	for i, w := range out {
		if v, ok := words[w]; ok {
			out[i] = v
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		out[i] = string(r)
	}
	return strings.Join(out, " ")
}

// splitDigits splits "fenix8" into "fenix", "8" and "fr965" into "fr", "965"; "310xt" keeps
// its letters with the number ("310XT").
func splitDigits(w string) []string {
	if w == "" {
		return nil
	}
	i := strings.IndexFunc(w, unicode.IsDigit)
	if i <= 0 {
		if i == 0 {
			return []string{strings.ToUpper(w)}
		}
		return []string{w}
	}
	return []string{w[:i], strings.ToUpper(w[i:])}
}
