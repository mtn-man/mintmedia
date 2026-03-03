package logging

// Event is the canonical identifier for a structured operational log event.
type Event string

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

	EventProcessorMoveMainApplied                Event = "processor.move.main.applied"
	EventProcessorMoveAssociatedApplied          Event = "processor.move.associated.applied"
	EventProcessorMoveAssociatedFailed           Event = "processor.move.associated.failed"
	EventProcessorCleanupSkippedAssociatedFailed Event = "processor.cleanup.skipped.associated.failed"
	EventProcessorCleanupSkippedFailed           Event = "processor.cleanup.skipped.failed"
	EventProcessorCleanupSourceFailed            Event = "processor.cleanup.source.failed"
	EventProcessorInputMaxDepthNoMedia           Event = "processor.input.max.depth.no.media"
	EventProcessorInputSkippedInputMissing       Event = "processor.input.skipped.input.missing"
	EventProcessorInputSkippedParseError         Event = "processor.input.skipped.parse.error"
	EventProcessorInputSkippedNotMedia           Event = "processor.input.skipped.not.media"
	EventProcessorInputSkippedNoMainMedia        Event = "processor.input.skipped.no.main.media"
	EventProcessorMoviePackSkipUnparseable       Event = "processor.movie.pack.skip.unparseable"
)

var reservedPathFields = map[string]struct{}{
	"path":         {},
	"src":          {},
	"dst":          {},
	"input_path":   {},
	"source_path":  {},
	"dest_path":    {},
	"drop_folder":  {},
	"movies_dir":   {},
	"shows_dir":    {},
	"history_file": {},
}

func isReservedPathField(key string) bool {
	_, ok := reservedPathFields[key]
	return ok
}

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
		EventProcessorMoveMainApplied,
		EventProcessorMoveAssociatedApplied,
		EventProcessorMoveAssociatedFailed,
		EventProcessorCleanupSkippedAssociatedFailed,
		EventProcessorCleanupSkippedFailed,
		EventProcessorCleanupSourceFailed,
		EventProcessorInputMaxDepthNoMedia,
		EventProcessorInputSkippedInputMissing,
		EventProcessorInputSkippedParseError,
		EventProcessorInputSkippedNotMedia,
		EventProcessorInputSkippedNoMainMedia,
		EventProcessorMoviePackSkipUnparseable,
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
		EventDaemonMagnetAdded,
		EventDaemonTxCleanupRemoved,
	}
}
