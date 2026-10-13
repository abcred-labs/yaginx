//go:build !linux

package upgrade

import "errors"

func withLock(_ string, _ func() error) error {
	return errors.New("yaginx upgrade locking is supported only on Linux")
}
