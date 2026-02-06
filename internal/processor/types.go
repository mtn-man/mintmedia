// internal/processor/types.go
package processor

import (
	"context"
	"errors"
	"fmt"
)

// Category represents Mintmedia's two canonical library targets.
type Category string

const (
	CategoryMovie Category = "Movies"
	CategoryShow  Category = "Shows"
)

// Request describes a single processing request. It is intentionally small.
// - InputPath can be a file or a directory.
// - CategoryHint is optional; if set, it should be CategoryMovie or CategoryShow.
type Request struct {
	InputPath    string
	CategoryHint Category
}

// Move describes an intended file move.
type Move struct {
	Source string // absolute or input-resolved source path
	Dest   string // absolute destination path
	Kind   string // "main" or "associated"
}

// Plan is the deterministic result of analyzing an input.
// It should be stable and testable, and should not depend on global state.
type Plan struct {
	// Input
	InputPath    string
	CategoryHint Category

	// Category decision
	Category Category

	// Main media selection
	MainSourcePath string // chosen main media file (may equal InputPath if InputPath is a file)
	MainExt        string // includes leading dot, e.g. ".mkv"
	MainBaseName   string // basename of MainSourcePath

	// Parsed identity (one of Movie or Show fields will be populated based on Category)
	MovieTitle string // e.g. "Get Smart (2008)"

	ShowName string // e.g. "Stranger Things"
	ShowYear string // e.g. "2016" or "" if unknown/not used
	Season   int    // e.g. 5
	Episode  int    // e.g. 8

	// Destination computation
	DestDir      string // directory containing main file
	DestRadix    string // base filename without extension used for main and associated files
	DestMainPath string // full destination path for the main file (DestDir + DestRadix + MainExt)

	// Associated files to move (if any)
	Associated []Move

	// Cleanup intent (optional; not all Apply implementations will honor this initially)
	DeleteEmptyInputDir bool
}

// Result reports the outcome of applying a plan.
type Result struct {
	Plan    Plan
	Applied bool

	// Handled indicates the processor intentionally handled the item without producing a library move
	// (e.g., quarantined non-media inputs, ignored unsupported items, etc.).
	Handled bool
	Reason  string
}

// PlanIssue captures a skipped path and the associated error.
type PlanIssue struct {
	Path string
	Err  error
}

// PartialPlanError indicates that some items were skipped but planning succeeded for others.
type PartialPlanError struct {
	Issues []PlanIssue
}

func (e *PartialPlanError) Error() string {
	return fmt.Sprintf("partial plan: %d item(s) skipped", len(e.Issues))
}

// ParseShowError indicates a failure to parse show info from names.
type ParseShowError struct {
	BaseName string
	FileName string
}

func (e *ParseShowError) Error() string {
	return fmt.Sprintf("failed to parse show info from %q or %q", e.BaseName, e.FileName)
}

// ParseMovieError indicates a failure to parse movie info from names.
type ParseMovieError struct {
	BaseName string
	FileName string
}

func (e *ParseMovieError) Error() string {
	return fmt.Sprintf("failed to parse movie info from %q or %q", e.BaseName, e.FileName)
}

// Processor is the core media decision+execution engine.
// Plan should be deterministic and side-effect free except for filesystem reads (stat/list).
// Apply performs the actual filesystem modifications (moves, quarantine, history writes).
type Processor interface {
	Plan(ctx context.Context, req Request) ([]Plan, error)
	Apply(ctx context.Context, plans []Plan) ([]Result, error)
	Process(ctx context.Context, req Request) ([]Result, error)
}

// Transferer moves a file from src -> dst.
// Implementations should try rename first and fall back to copy+atomic finalize on cross-filesystem.
type Transferer interface {
	Move(ctx context.Context, src, dst string) error
}

// Quarantiner moves a file or directory into the configured error/quarantine location.
type Quarantiner interface {
	Quarantine(ctx context.Context, src string, reason string) (dest string, err error)
}

// HistoryWriter records events (moves/quarantine/etc.) for later auditing/debugging.
type HistoryWriter interface {
	Append(ctx context.Context, entry string) error
}

// Config contains the processor-relevant configuration.
// This is a "resolved" config: paths should be absolute and validated.
type Config struct {
	DropFolder string

	MoviesDir string
	ShowsDir  string

	ErrorDir    string
	HistoryFile string

	MainMediaExtensions      []string // includes leading dots
	AssociatedFileExtensions []string // includes leading dots

	// Naming: patterns used to strip junk tags from release names.
	// These are regex patterns expressed as strings (ideally compiled once during processor init).
	MediaTagBlacklist []string
}

// NoMainMediaFoundError wraps ErrNoMainMediaFound and carries depth context.
type NoMainMediaFoundError struct {
	Path     string
	MaxDepth int
	DepthHit bool
}

func (e *NoMainMediaFoundError) Error() string {
	return ErrNoMainMediaFound.Error()
}

func (e *NoMainMediaFoundError) Unwrap() error {
	return ErrNoMainMediaFound
}

// Sentinel errors used by Plan() so higher layers (worker/watch) can decide what to do.
var (
	// ErrNotMedia indicates the input is not a recognized main media type.
	ErrNotMedia = errors.New("not a main media file")

	// ErrNoMainMediaFound indicates a directory input did not contain a recognized main media file.
	ErrNoMainMediaFound = errors.New("no main media found in directory")

	// ErrInputMissing indicates the input path was removed before processing.
	ErrInputMissing = errors.New("input path no longer exists")

	// ErrUncategorized indicates the processor could not determine Movies vs Shows.
	ErrUncategorized = errors.New("unable to categorize media")

	// ErrAmbiguousShow indicates multiple possible show folders matched with no clear choice.
	ErrAmbiguousShow = errors.New("ambiguous show folder match")
)
