package logging

// Event is the canonical identifier for a structured operational log event.
type Event string

// Canonical event identifiers logged across the daemon/CLI.
const (
	EventSystemStartup              Event = "system.startup"
	EventSystemDestinationsReady    Event = "system.destinations.ready"
	EventSystemDestinationsWaiting  Event = "system.destinations.waiting"
	EventSystemShutdownRequested    Event = "system.shutdown.requested"
	EventSystemShutdownGraceElapsed Event = "system.shutdown.grace.elapsed"
	EventSystemShutdownComplete     Event = "system.shutdown.complete"
	EventSystemShutdownTimeout      Event = "system.shutdown.timeout"

	EventDaemonPathDuplicate    Event = "daemon.path.duplicate"
	EventDaemonWatchError       Event = "daemon.watch.error"
	EventDaemonClipboardError   Event = "daemon.clipboard.error"
	EventDaemonTxAddError       Event = "daemon.tx.add.error"
	EventDaemonMagnetAdded      Event = "daemon.magnet.added"
	EventDaemonProcessError     Event = "daemon.process.error"
	EventDaemonTxCleanupError   Event = "daemon.tx.cleanup.error"
	EventDaemonTxCleanupRemoved Event = "daemon.tx.cleanup.removed"

	EventDaemonDestinationDegraded  Event = "daemon.destination.degraded"
	EventDaemonDestinationRecovered Event = "daemon.destination.recovered"
	EventDaemonDestinationDeferred  Event = "daemon.destination.deferred"

	EventProcessorMoveMainApplied                Event = "processor.move.main.applied"
	EventProcessorMoveAssociatedApplied          Event = "processor.move.associated.applied"
	EventProcessorMoveAssociatedFailed           Event = "processor.move.associated.failed"
	EventProcessorCleanupSkippedAssociatedFailed Event = "processor.cleanup.skipped.associated.failed"
	EventProcessorCleanupSkippedDuplicate        Event = "processor.cleanup.skipped.duplicate"
	EventProcessorCleanupSkippedFailed           Event = "processor.cleanup.skipped.failed"
	EventProcessorCleanupSourceFailed            Event = "processor.cleanup.source.failed"
	EventProcessorInputMaxDepthNoMedia           Event = "processor.input.max.depth.no.media"
	EventProcessorInputSkippedInputMissing       Event = "processor.input.skipped.input.missing"
	EventProcessorInputSkippedParseError         Event = "processor.input.skipped.parse.error"
	EventProcessorInputSkippedNotMedia           Event = "processor.input.skipped.not.media"
	EventProcessorInputSkippedNoMainMedia        Event = "processor.input.skipped.no.main.media"
	EventProcessorInputSkippedDuplicate          Event = "processor.input.skipped.duplicate"
	EventProcessorMoviePackSkipUnparseable       Event = "processor.movie.pack.skip.unparseable"
	EventProcessorShowFolderQualifiedGuess       Event = "processor.show.folder.qualified.guess"
	EventProcessorShowFileSkipUnparseable        Event = "processor.show.file.skip.unparseable"
)

// AllOperationalEvents returns the complete set of production event constants.
func AllOperationalEvents() []Event {
	return []Event{
		EventSystemStartup,
		EventSystemDestinationsReady,
		EventSystemDestinationsWaiting,
		EventSystemShutdownRequested,
		EventSystemShutdownGraceElapsed,
		EventSystemShutdownComplete,
		EventSystemShutdownTimeout,
		EventDaemonPathDuplicate,
		EventDaemonWatchError,
		EventDaemonClipboardError,
		EventDaemonTxAddError,
		EventDaemonMagnetAdded,
		EventDaemonProcessError,
		EventDaemonTxCleanupError,
		EventDaemonTxCleanupRemoved,
		EventDaemonDestinationDegraded,
		EventDaemonDestinationRecovered,
		EventDaemonDestinationDeferred,
		EventProcessorMoveMainApplied,
		EventProcessorMoveAssociatedApplied,
		EventProcessorMoveAssociatedFailed,
		EventProcessorCleanupSkippedAssociatedFailed,
		EventProcessorCleanupSkippedDuplicate,
		EventProcessorCleanupSkippedFailed,
		EventProcessorCleanupSourceFailed,
		EventProcessorInputMaxDepthNoMedia,
		EventProcessorInputSkippedInputMissing,
		EventProcessorInputSkippedParseError,
		EventProcessorInputSkippedNotMedia,
		EventProcessorInputSkippedNoMainMedia,
		EventProcessorInputSkippedDuplicate,
		EventProcessorMoviePackSkipUnparseable,
		EventProcessorShowFolderQualifiedGuess,
		EventProcessorShowFileSkipUnparseable,
	}
}

// DefaultHistoryInfoAllowlist returns info events that are persisted when
// history level is WARN.
func DefaultHistoryInfoAllowlist() []Event {
	return []Event{
		EventSystemStartup,
		EventSystemShutdownRequested,
		EventSystemShutdownComplete,
		EventSystemShutdownTimeout,
		EventSystemDestinationsWaiting,
		EventSystemDestinationsReady,
		EventProcessorMoveMainApplied,
		EventProcessorMoveAssociatedApplied,
		EventProcessorInputSkippedNotMedia,
		EventProcessorInputSkippedNoMainMedia,
		EventProcessorInputSkippedInputMissing,
		EventProcessorInputSkippedParseError,
		EventProcessorInputSkippedDuplicate,
		EventDaemonMagnetAdded,
		EventDaemonTxCleanupRemoved,
		EventDaemonPathDuplicate,
		EventDaemonDestinationRecovered,
		EventDaemonDestinationDeferred,
	}
}
