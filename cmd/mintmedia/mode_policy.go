package main

import (
	"errors"
	"strings"
)

var errConflictingModes = errors.New("use only one of --plan (or --dry-run), --plan <path>, --process, --process-drop, or --daemon at a time")
var errProcessingDisabled = errors.New("processing modes require features.enable_processing=true in the config.toml; set it to true or run with no mode flags for a config-only smoke test")

type modePolicy struct {
	PlanPath      string
	PlanDrop      bool
	ProcessPath   string
	ProcessDrop   bool
	Daemon        bool
	ExplicitCount int
}

func resolveModePolicy(
	planPath string,
	planDrop bool,
	processPath string,
	processDrop bool,
	daemon bool,
	enableProcessing bool,
) (modePolicy, error) {
	mode := modePolicy{
		PlanPath:    strings.TrimSpace(planPath),
		PlanDrop:    planDrop,
		ProcessPath: strings.TrimSpace(processPath),
		ProcessDrop: processDrop,
		Daemon:      daemon,
	}

	if mode.PlanPath != "" {
		mode.ExplicitCount++
	}
	if mode.PlanDrop {
		mode.ExplicitCount++
	}
	if mode.ProcessPath != "" {
		mode.ExplicitCount++
	}
	if mode.ProcessDrop {
		mode.ExplicitCount++
	}
	if mode.Daemon {
		mode.ExplicitCount++
	}

	if mode.ExplicitCount > 1 {
		return modePolicy{}, errConflictingModes
	}

	if !enableProcessing {
		if mode.ExplicitCount > 0 {
			return modePolicy{}, errProcessingDisabled
		}
		return mode, nil
	}

	if mode.ExplicitCount == 0 {
		mode.ProcessDrop = true
	}

	return mode, nil
}
