package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mtn-man/mintmedia/internal/config"
	"github.com/mtn-man/mintmedia/internal/daemon"
	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/state"
	"github.com/mtn-man/mintmedia/internal/transmission"
	"github.com/mtn-man/mintmedia/internal/watch"
)

const lockFilename = "mintmedia.lock"

func runDaemonMode(resolved *config.Resolved, proc processor.Processor, logger logging.Logger) (bool, error) {
	lockPath := filepath.Join(resolved.StateDirAbs, lockFilename)
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

	var tx *transmission.Client
	if resolved.TorrentEnabled {
		tx = &transmission.Client{
			Host: resolved.TorrentHost,
			Auth: resolved.TorrentAuth,
		}
	}

	d := &daemon.Daemon{
		Watcher: w,
		Proc:    proc,
		Tx:      tx,
		Logger:  logger,

		MoviesDir: resolved.DestDirMoviesAbs,
		ShowsDir:  resolved.DestDirShowsAbs,

		DeferDestinationChecks: resolved.DeferDestinationChecks,

		SoundDone:             defaultSoundDone,
		DoneNotificationMode:  resolved.DoneNotificationMode,
		ShutdownGraceDuration: resolved.ShutdownGraceDuration,
		ShutdownForceTimeout:  resolved.ShutdownForceTimeout,

		AutoCleanupCompletedTorrents: resolved.AutoCleanupCompletedTorrents,
		CleanupCooldown:              defaultCleanupCooldown,
	}

	if err := d.Run(runCtx); err != nil {
		return false, err
	}

	return runCtx.Err() != nil, nil
}
