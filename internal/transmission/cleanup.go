package transmission

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RemoveCompleted removes all torrents that are 100% complete from Transmission.
// It removes the torrent entries from the list (does NOT delete local data).
//
// Returns the number of torrents removed.
//
// Implementation strategy:
//  1. transmission-remote <host> [-n user:pass] -l
//  2. parse rows where Done column == "100%"
//  3. transmission-remote <host> [-n user:pass] -t <id> -r
func (c Client) RemoveCompleted(ctx context.Context) (int, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}

	// Obey caller context exactly. The caller chooses whether to detach from parent
	// cancellation and/or apply a timeout.

	remote := strings.TrimSpace(c.RemotePath)
	if remote == "" {
		remote = "transmission-remote"
	}

	ids, err := c.listCompletedIDs(ctx, remote)
	if err != nil {
		return 0, fmt.Errorf("list completed torrents: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	removed := 0
	for _, id := range ids {
		if err := c.removeTorrent(ctx, remote, id); err != nil {
			return removed, fmt.Errorf("remove torrent (id=%d): %w", id, err)
		}
		removed++
	}

	return removed, nil
}

func (c Client) listCompletedIDs(ctx context.Context, remote string) ([]int, error) {
	args := c.baseArgs()
	args = append(args, "-l")

	cmd := exec.CommandContext(ctx, remote, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("transmission-remote -l failed: %w", ctxErr)
		}
		return nil, fmt.Errorf("transmission-remote -l failed: %w; output: %s", err, strings.TrimSpace(string(out)))
	}

	var ids []int
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip header and summary lines.
		// Common patterns:
		// "ID Done Have ETA Up Down Ratio Status Name"
		// "Sum: ..."
		if strings.HasPrefix(line, "ID ") || strings.HasPrefix(line, "Sum:") {
			continue
		}

		fields := strings.Fields(line)
		// Expect: ID Done ...
		// Example: "  1 100%  1.23 GB  Done  0.0  0.0  1.0  Idle  Name"
		if len(fields) < 2 {
			continue
		}

		idStr := fields[0]
		doneStr := fields[1]

		id, convErr := strconv.Atoi(idStr)
		if convErr != nil {
			continue
		}

		if doneStr == "100%" {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

func (c Client) removeTorrent(ctx context.Context, remote string, id int) error {
	args := c.baseArgs()
	// -t <id> selects torrent, -r removes torrent from list (does not delete data)
	args = append(args, "-t", strconv.Itoa(id), "-r")

	cmd := exec.CommandContext(ctx, remote, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("transmission-remote remove failed (id=%d): %w", id, ctxErr)
		}
		return fmt.Errorf("transmission-remote remove failed (id=%d): %w; output: %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// baseArgs returns args that should be included for all transmission-remote calls:
// host + optional auth.
func (c Client) baseArgs() []string {
	args := []string{c.Host}
	if strings.TrimSpace(c.Auth) != "" {
		// transmission-remote uses -n user:pass
		args = append(args, "-n", c.Auth)
	}
	return args
}
