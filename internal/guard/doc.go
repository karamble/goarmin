// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package guard holds repository-wide tests that enforce project rules: every outbound HTTP
// client comes from pkg/httpx (so it carries a browser User-Agent), and no test or test fixture
// hardcodes a date. It contains no production code.
package guard
