package transmission

import (
	"context"
	"encoding/json"
	"fmt"
)

// RemoveCompleted removes all 100%-complete torrents from Transmission.
// It removes the queue entry only — local data is not deleted.
// Returns the number of entries removed.
func (c *Client) RemoveCompleted(ctx context.Context) (int, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}

	ids, err := c.listCompletedIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list completed torrents: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	removed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
		if err := c.removeTorrent(ctx, id); err != nil {
			return removed, fmt.Errorf("remove torrent (id=%d): %w", id, err)
		}
		removed++
	}
	return removed, nil
}

func (c *Client) listCompletedIDs(ctx context.Context) ([]int, error) {
	args, err := c.rpc(ctx, "torrent-get", map[string]interface{}{
		"fields": []string{"id", "percentDone"},
	})
	if err != nil {
		return nil, err
	}

	if args == nil {
		return nil, nil
	}

	var result struct {
		Torrents []struct {
			ID          int     `json:"id"`
			PercentDone float64 `json:"percentDone"`
		} `json:"torrents"`
	}
	if err := json.Unmarshal(args, &result); err != nil {
		return nil, fmt.Errorf("parse torrent list: %w", err)
	}

	var ids []int
	for _, t := range result.Torrents {
		if t.PercentDone >= 1.0 {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

func (c *Client) removeTorrent(ctx context.Context, id int) error {
	_, err := c.rpc(ctx, "torrent-remove", map[string]interface{}{
		"ids":               []int{id},
		"delete-local-data": false,
	})
	return err
}
