package main

import (
	"fmt"
	"os"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/notify"
)

var newMainCaffeinate = func() notify.CaffeinateController {
	return notify.NewCaffeinate()
}

// cliCaffeinateHooks builds the notify.CaffeinateHooks shared by every CLI
// one-shot path (--process, --process-drop): plain console output, wording
// shared with the daemon via notify's Caffeinate*Message/Warning helpers.
func cliCaffeinateHooks() notify.CaffeinateHooks {
	return notify.CaffeinateHooks{
		OnUnsupported: func() {
			fmt.Println(console.ColorizePrefixOut(notify.CaffeinateUnsupportedMessage))
		},
		OnStartWarn: func(err error) {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(notify.CaffeinateStartWarning(err)))
		},
		OnStopWarn: func(err error) {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(notify.CaffeinateStopWarning(err)))
		},
	}
}
