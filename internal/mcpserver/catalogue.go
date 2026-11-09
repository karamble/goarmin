// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

// Excluded are the API routes MCP leaves out on purpose, with the reason. Everything else the
// API serves is a tool or a resource; the api package's guard test fails on a route that is
// neither.
var Excluded = map[string]string{
	"GET /api/health":                      "a liveness check",
	"GET /healthz":                         "a liveness check",
	"GET /api/setup":                       "first-run setup creates the first account (credentials)",
	"POST /api/setup":                      "first-run setup creates the first account (credentials)",
	"POST /api/auth/login":                 "signing in (credentials)",
	"POST /api/auth/logout":                "signing out (sessions)",
	"POST /api/auth/passkey/login/begin":   "signing in (credentials)",
	"POST /api/auth/passkey/login/finish":  "signing in (credentials)",
	"GET /api/invites/{token}":             "accepting an invite creates an account (credentials)",
	"POST /api/invites/{token}/accept":     "accepting an invite creates an account (credentials)",
	"POST /api/me/password":                "the password (credentials)",
	"GET /api/me/sessions":                 "sign-in sessions (credentials)",
	"DELETE /api/me/sessions/{id}":         "sign-in sessions (credentials)",
	"GET /api/me/passkeys":                 "passkeys (credentials)",
	"DELETE /api/me/passkeys/{id}":         "passkeys (credentials)",
	"POST /api/me/passkeys/begin":          "passkeys (credentials)",
	"POST /api/me/passkeys/finish":         "passkeys (credentials)",
	"GET /api/me/mcp":                      "the MCP tokens themselves (credentials)",
	"POST /api/me/mcp/tokens":              "the MCP tokens themselves (credentials)",
	"PATCH /api/me/mcp/tokens/{id}":        "the MCP tokens themselves (credentials)",
	"DELETE /api/me/mcp/tokens/{id}":       "the MCP tokens themselves (credentials)",
	"GET /tiles/":                          "map images for the dashboard's maps",
	"/api/":                                "the answer for unknown API routes",
	"/mcp":                                 "MCP itself",
	"/":                                    "the dashboard's pages",
	"/api/node/v1/":                        "the answer for unknown node routes",
	"GET /api/node/v1/enroll/{poll}":       "spoken by sync nodes, not people",
	"POST /api/node/v1/login":              "spoken by sync nodes, not people",
	"POST /api/node/v1/enroll":             "spoken by sync nodes, not people",
	"POST /api/node/v1/hello":              "spoken by sync nodes, not people",
	"PUT /api/node/v1/files/{sha256}":      "spoken by sync nodes, not people",
	"POST /api/node/v1/stored":             "spoken by sync nodes, not people",
	"POST /api/node/v1/runs":               "spoken by sync nodes, not people",
	"POST /api/node/v1/inventory":          "spoken by sync nodes, not people",
	"GET /api/node/v1/history":             "spoken by sync nodes, not people",
	"GET /api/node/v1/outbox":              "spoken by sync nodes, not people",
	"POST /api/node/v1/sent":               "spoken by sync nodes, not people",
	"POST /api/node/v1/progress":           "spoken by sync nodes, not people",
	"GET /api/node/v1/commands":            "spoken by sync nodes, not people",
	"POST /api/node/v1/commands/{id}":      "spoken by sync nodes, not people",
	"POST /api/node/v1/live":               "spoken by sync nodes, not people",
	"POST /api/node/v1/pairing":            "spoken by sync nodes, not people",
	"POST /api/node/v1/pairing/owners":     "spoken by sync nodes, not people",
	"POST /api/node/v1/pairing/identified": "spoken by sync nodes, not people",
	"POST /api/node/v1/pairing/claim":      "spoken by sync nodes, not people",
}

// Covered reports whether MCP offers an API route, as a tool, a resource, or a stream a tool
// polls.
func Covered(route string) bool {
	for i := range Tools {
		if Tools[i].Route() == route {
			return true
		}
		for _, c := range Tools[i].Covers {
			if c == route {
				return true
			}
		}
	}
	for i := range Resources {
		if Resources[i].Route() == route {
			return true
		}
	}
	return false
}

// Item is one entry of the catalogue.
type Item struct {
	Kind        string `json:"kind"` // tool, resource, prompt
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Area        string `json:"area"`
	Level       string `json:"level"`
	Destructive bool   `json:"destructive,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	URI         string `json:"uri,omitempty"`
	Route       string `json:"route,omitempty"`
}

// Catalogue is everything MCP offers, for the Settings tab and goarmin mcp -list.
func Catalogue() []Item {
	var out []Item
	for _, t := range Tools {
		out = append(out, Item{Kind: "tool", Name: t.Name, Title: t.Title, Description: t.Desc, Area: t.Area, Level: t.Level.String(),
			Destructive: t.Destructive, ReadOnly: t.ReadOnly, Route: t.Route()})
	}
	for _, r := range Resources {
		out = append(out, Item{Kind: "resource", Name: r.Name, Title: r.Title, Description: r.Desc, Area: "Files", Level: Read.String(),
			ReadOnly: true, URI: r.Template, Route: r.Route()})
	}
	for _, p := range Prompts {
		out = append(out, Item{Kind: "prompt", Name: p.Name, Title: p.Title, Description: p.Desc, Area: "Prompts", Level: Read.String()})
	}
	return out
}
