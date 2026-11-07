// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build unix

package store

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

var errLocked = errors.New("locked by another process")

func tryLockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // fd fits in int
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // fd fits in int
}
