// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

//go:build windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const restartManagerMoreData = 234

var restartManager = syscall.NewLazyDLL("rstrtmgr.dll")

type restartManagerProcess struct {
	processID       uint32
	processStart    syscall.Filetime
	appName         [256]uint16
	serviceName     [64]uint16
	applicationType uint32
	appStatus       uint32
	tssessionID     uint32
	restartable     int32
}

// fileLockOwners reports processes that Windows Restart Manager identifies as
// using path. It is called only after the strict pre-exit deletion assertion
// fails, so diagnostics cannot turn a locked database into a passing test.
func fileLockOwners(path string) (string, error) {
	start := restartManager.NewProc("RmStartSession")
	register := restartManager.NewProc("RmRegisterResources")
	getList := restartManager.NewProc("RmGetList")
	end := restartManager.NewProc("RmEndSession")
	for _, proc := range []*syscall.LazyProc{start, register, getList, end} {
		if err := proc.Find(); err != nil {
			return "", err
		}
	}

	var session uint32
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		return "", fmt.Errorf("generate Restart Manager session key: %w", err)
	}
	key, err := syscall.UTF16FromString(hex.EncodeToString(keyBytes[:]))
	if err != nil {
		return "", err
	}
	result, _, _ := start.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	runtime.KeepAlive(key)
	if result != 0 {
		return "", syscall.Errno(result)
	}
	defer end.Call(uintptr(session))

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	files := []uintptr{uintptr(unsafe.Pointer(pathPtr))}
	result, _, _ = register.Call(uintptr(session), 1, uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0)
	if result != 0 {
		return "", syscall.Errno(result)
	}

	var needed, count, rebootReasons uint32
	result, _, _ = getList.Call(
		uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), 0,
		uintptr(unsafe.Pointer(&rebootReasons)),
	)
	if result != restartManagerMoreData && result != 0 {
		return "", syscall.Errno(result)
	}
	if needed == 0 {
		return "Restart Manager reported no process owners", nil
	}

	processes := make([]restartManagerProcess, needed)
	count = needed
	result, _, _ = getList.Call(
		uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&processes[0])), uintptr(unsafe.Pointer(&rebootReasons)),
	)
	runtime.KeepAlive(pathPtr)
	runtime.KeepAlive(files)
	if result != 0 {
		return "", syscall.Errno(result)
	}
	if count > uint32(len(processes)) {
		return "", fmt.Errorf("Restart Manager returned %d processes for a %d-entry buffer", count, len(processes))
	}

	owners := make([]string, 0, count)
	for _, process := range processes[:count] {
		name := syscall.UTF16ToString(process.appName[:])
		owners = append(owners, fmt.Sprintf("pid=%d app=%q", process.processID, name))
	}
	return fmt.Sprint(owners), nil
}
