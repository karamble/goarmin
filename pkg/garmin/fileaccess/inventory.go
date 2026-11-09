// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// Inventory lists every item of the given GDXML types ("FIT_TYPE_5") the watch keeps now,
// synced or not. It is read-only: the store, its cursor and the watch's flags stay as they are,
// so it may run after a Sync on the same connection to learn, say, which workouts are on the
// watch. If the watch refuses the type filter, it lists everything once and filters itself.
func (e *Engine) Inventory(ctx context.Context, types []string) ([]Item, error) {
	if len(types) == 0 {
		return nil, errors.New("fileaccess: an inventory needs at least one type")
	}
	items, err := e.listAll(ctx, InventoryRequest(types), types)
	var st listStatusError
	if errors.As(err, &st) {
		e.log.Info("fileaccess: the watch refused a listing by type; listing everything", "status", st.status)
		items, err = e.listAll(ctx, ListAllRequest(nil), types)
	}
	return items, err
}

// listStatusError is a listing the watch answered with a failure status.
type listStatusError struct {
	page   int
	status smart.ItemListStatus
}

func (e listStatusError) Error() string {
	return fmt.Sprintf("fileaccess: inventory page %d: %s", e.page, e.status)
}

// listAll runs one listing from req (all pages) and keeps the items whose type is in types.
func (e *Engine) listAll(ctx context.Context, req smart.FileAccess, types []string) ([]Item, error) {
	var items []Item
	for page := 0; ; page++ {
		if page >= 10000 {
			return nil, errors.New("fileaccess: listing does not end")
		}
		resp, err := e.call(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("fileaccess: inventory page %d: %w", page, err)
		}
		lr := resp.ItemListResponse
		if lr == nil {
			return nil, fmt.Errorf("fileaccess: inventory page %d: answer is %s, not ItemListResponse", page, resp.Kind())
		}
		if lr.Status != nil && *lr.Status != smart.ItemListSuccess {
			return nil, listStatusError{page: page, status: *lr.Status}
		}
		for _, ref := range lr.Items {
			if it, ok := e.item(ref); ok && slices.Contains(types, it.TypeName) {
				items = append(items, it)
			}
		}
		if lr.SessionID == nil {
			return items, nil
		}
		req = ContinueListRequest(*lr.SessionID)
	}
}
