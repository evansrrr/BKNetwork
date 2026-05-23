//go:build !windows

package main

import "errors"

var errElevationCanceled = errors.New("elevation canceled")

func ensureElevatedAtStartup() (bool, error) {
	return false, nil
}
