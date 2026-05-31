package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mtn-man/mintmedia/internal/clipboard"
	"github.com/mtn-man/mintmedia/internal/config"
	"github.com/mtn-man/mintmedia/internal/daemon"
	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/state"
	"github.com/mtn-man/mintmedia/internal/transmission"
	"github.com/mtn-man/mintmedia/internal/watch"
)

func runDaemonMode(cfg *config.Config, resolved *config.Resolved, proc processor.Processor, logger logging.Logger) (bool, error) {
	lockPath := filepath.Join(resolved.StateDirAbs, "mintmedia.lock")
	releaseLock, err := state.AcquireLock(lockPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = releaseLock() }()

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w, err := watch.NewDropFolderWatcher(resolved.DropFolderAbs, resolved.DropSettleDuration)
	if err != nil {
		return false, err
	}

	torrentEnabled := cfg.Features.EnableTorrentAutomation && cfg.Torrent.Enabled

	var poller *clipboard.Poller
	if torrentEnabled && cfg.Clipboard.Enabled {
		poller, err = clipboard.NewPoller(resolved.ClipboardPollInterval)
		if err != nil {
			if errors.Is(err, clipboard.ErrUnsupportedPlatform) {
				return false, fmt.Errorf(
					"clipboard polling is enabled but not available: %w",
					err,
				)
			}
			return false, err
		}
	}

	var tx *transmission.Client
	if torrentEnabled {
		tx = &transmission.Client{
			Host: cfg.Torrent.Host,
			Auth: cfg.Torrent.Auth,
		}
	}

	autoCleanupCompletedTorrents := false
	if cfg.Torrent.AutoCleanupCompletedTorrents != nil {
		autoCleanupCompletedTorrents = *cfg.Torrent.AutoCleanupCompletedTorrents
	}

	d := &daemon.Daemon{
		Watcher: w,
		Poller:  poller,
		Proc:    proc,
		Tx:      tx,
		Logger:  logger,

		TransmissionHost: cfg.Torrent.Host,

		MoviesDir: resolved.DestDirMoviesAbs,
		ShowsDir:  resolved.DestDirShowsAbs,

		DeferDestinationChecks: cfg.System.DeferDestinationChecks,

		SoundInput:            defaultSoundInput,
		SoundDone:             defaultSoundDone,
		DoneNotificationMode:  resolved.DoneNotificationMode,
		ShutdownGraceDuration: resolved.ShutdownGraceDuration,
		ShutdownForceTimeout:  resolved.ShutdownForceTimeout,

		MagnetTimeout: defaultMagnetTimeout,

		AutoCleanupCompletedTorrents: autoCleanupCompletedTorrents,
		CleanupCooldown:              defaultCleanupCooldown,
	}

	if err := d.Run(runCtx); err != nil {
		return false, err
	}

	return runCtx.Err() != nil, nil
}
