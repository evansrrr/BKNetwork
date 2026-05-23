//go:build windows

package main

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errElevationCanceled = errors.New("elevation canceled")

func ensureElevatedAtStartup() (bool, error) {
	isAdmin, err := isCurrentProcessElevated()
	if err != nil {
		return false, err
	}
	if isAdmin {
		return false, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}

	args := make([]string, 0, len(os.Args)-1)
	for _, arg := range os.Args[1:] {
		args = append(args, syscall.EscapeArg(arg))
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return false, err
	}
	file, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return false, err
	}
	params, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return false, err
	}
	dir, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return false, err
	}

	r1, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(windows.SW_NORMAL),
	)
	if r1 <= 32 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == 1223 {
			return false, errElevationCanceled
		}
		if callErr == syscall.Errno(0) {
			return false, syscall.EINVAL
		}
		return false, callErr
	}

	return true, nil
}

func isCurrentProcessElevated() (bool, error) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false, err
	}
	defer token.Close()

	var elevation uint32
	var outLen uint32
	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&outLen,
	)
	if err != nil {
		return false, err
	}
	return elevation != 0, nil
}

var (
	shell32DLL        = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW = shell32DLL.NewProc("ShellExecuteW")
)
