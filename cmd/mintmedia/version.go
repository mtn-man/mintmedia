package main

import (
	"fmt"
	"runtime/debug"
)

const (
	defaultVersion        = "dev"
	develBuildInfoVersion = "(devel)"
)

// version is overridden in release builds via:
// go build -ldflags "-X main.version=vX.Y.Z"
var version = defaultVersion

func formatVersionLine(v string) string {
	return fmt.Sprintf("mintmedia %s\n", v)
}

func mainModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return info.Main.Version
}

func resolveVersion(buildVersion, moduleVersion string) string {
	if buildVersion != "" && buildVersion != defaultVersion {
		return buildVersion
	}
	if moduleVersion != "" && moduleVersion != develBuildInfoVersion {
		return moduleVersion
	}
	return defaultVersion
}
