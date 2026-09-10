package main

import "syscall"

// grantableModeBits are the permission bits mintmedia must be able to grant,
// and therefore the umask bits it clears at startup: owner rwx, plus group and
// other read+execute. Directories need execute to be traversable and read to be
// listable; files need read.
//
// Group and other *write* (0o022) is deliberately not included -- mintmedia
// never requests it, so an operator's choice to mask it survives untouched.
const grantableModeBits = 0o755

// relaxUmaskForLibraryAccess clears the umask bits that would stop a media
// server -- typically running as a different user -- from reading what
// mintmedia sorts.
//
// A process inherits its umask from whatever launched it (systemd, launchd,
// cron, a shell), so without this the permissions of a sorted library depend on
// how mintmedia happened to be started: os.MkdirAll(dir, 0o755) under umask 077
// produces a 0700 folder the media server cannot enter, and the requested 0755
// is silently ignored.
//
// It clears only grantableModeBits rather than forcing a fixed umask, so
// whatever else the operator masked survives: group and other write stay masked
// under 077, because mintmedia never asks for them. Because a umask only ever
// removes permission bits, loosening one cannot expose anything the code
// creates deliberately private -- the config file, which may hold torrent
// credentials, is written 0o600 and stays 0o600.
//
// Owner bits are included in the mask-clear on purpose. Leaving them would let a
// pathological umask such as 0o700 strip the process's own access, landing that
// same config file at 0o000 -- the tool must be able to read what it writes.
//
// Startup-only: the umask is process-global, so it must be set before any
// goroutine runs, and the momentary Umask(0) used to read the current value is
// safe only because nothing is creating files yet.
func relaxUmaskForLibraryAccess() {
	current := syscall.Umask(0)
	syscall.Umask(current &^ grantableModeBits)
}
