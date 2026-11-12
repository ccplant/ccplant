package sessionstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const (
	volumeLockName    = ".session-state-lock"
	VolumeTempPattern = ".session-state-*.tmp"
)

func LockVolume(dir string) (func(), error) {
	release, _, err := lockVolume(dir, true)
	return release, err
}

func RemoveStaleVolumeTemps(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, VolumeTempPattern))
	if err != nil {
		return err
	}
	var errs []error
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func TryRemoveStaleVolumeTemps(dir string) (bool, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	release, acquired, err := lockVolume(dir, false)
	if err != nil || !acquired {
		return false, err
	}
	defer release()
	return true, RemoveStaleVolumeTemps(dir)
}

func lockVolume(dir string, wait bool) (func(), bool, error) {
	lock, err := os.OpenFile(filepath.Join(dir, volumeLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	for {
		err = syscall.Flock(int(lock.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = lock.Close() }, true, nil
}
