package main

import (
	"errors"
	"strings"
)

var errConflictingModes = errors.New("use only one of --plan, --apply, --process, --process-drop, or --daemon at a time")
var errProcessingDisabled = errors.New("processing modes require features.enable_processing=true in the config.toml; set it to true or run with no mode flags for a config-only smoke test")

type modePolicy struct {
	PlanPath      string
	ApplyPath     string
	ProcessPath   string
	ProcessDrop   bool
	Daemon        bool
	ExplicitCount int
}

func resolveModePolicy(
	planPath string,
	applyPath string,
	processPath string,
	processDrop bool,
	daemon bool,
	enableProcessing bool,
) (modePolicy, error) {
	mode := modePolicy{
		PlanPath:    strings.TrimSpace(planPath),
		ApplyPath:   strings.TrimSpace(applyPath),
		ProcessPath: strings.TrimSpace(processPath),
		ProcessDrop: processDrop,
		Daemon:      daemon,
	}

	if mode.PlanPath != "" {
		mode.ExplicitCount++
	}
	if mode.ApplyPath != "" {
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
