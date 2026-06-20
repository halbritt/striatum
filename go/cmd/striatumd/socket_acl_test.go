package main

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGrantDaemonSocketAccessToLaneUserSkipsWhenEnvUnset(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "")
	restore := stubDaemonSocketACL(t)
	defer restore()

	if err := grantDaemonSocketAccessToLaneUser("/run/user/1000/striatum/daemon-go.sock"); err != nil {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v", err)
	}
	if len(daemonSocketACLCalls) != 0 {
		t.Fatalf("setfacl should not run without %s, got %#v", daemonLaneOSUserEnv, daemonSocketACLCalls)
	}
}

func TestGrantDaemonSocketAccessToLaneUserSkipsWhenLaneUserIsDaemonUser(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "daemonuser")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }

	if err := grantDaemonSocketAccessToLaneUser("/run/user/1000/striatum/daemon-go.sock"); err != nil {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v", err)
	}
	if len(daemonSocketACLCalls) != 0 {
		t.Fatalf("setfacl should not run for same OS user, got %#v", daemonSocketACLCalls)
	}
}

func TestClearDaemonSocketAccessFromLaneUserRemovesRuntimeDirACL(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "striatum-lane")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }

	socketPath := filepath.Join(string(filepath.Separator), "run", "user", "1000", "striatum", "daemon-go.sock")
	clearDaemonSocketAccessFromLaneUser(socketPath)

	want := []daemonSocketACLCall{
		{op: "remove", spec: "u:striatum-lane", path: socketPath},
		{op: "remove", spec: "u:striatum-lane", path: filepath.Join(string(filepath.Separator), "run", "user", "1000", "striatum")},
		{op: "remove", spec: "u:striatum-lane", path: filepath.Join(string(filepath.Separator), "run", "user", "1000")},
	}
	if !reflect.DeepEqual(daemonSocketACLCalls, want) {
		t.Fatalf("setfacl calls = %#v, want %#v", daemonSocketACLCalls, want)
	}
}

func TestGrantDaemonSocketAccessToLaneUserAppliesParentDirSocketDirAndSocketACLs(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "striatum-lane")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }

	socketPath := filepath.Join(string(filepath.Separator), "run", "user", "1000", "striatum", "daemon-go.sock")
	if err := grantDaemonSocketAccessToLaneUser(socketPath); err != nil {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v", err)
	}
	want := []daemonSocketACLCall{
		{op: "set", spec: "u:striatum-lane:--x", path: filepath.Join(string(filepath.Separator), "run", "user", "1000")},
		{op: "set", spec: "u:striatum-lane:--x", path: filepath.Join(string(filepath.Separator), "run", "user", "1000", "striatum")},
		{op: "set", spec: "u:striatum-lane:rw-", path: socketPath},
	}
	if !reflect.DeepEqual(daemonSocketACLCalls, want) {
		t.Fatalf("setfacl calls = %#v, want %#v", daemonSocketACLCalls, want)
	}
}

func TestGrantDaemonSocketAccessToLaneUserSkipsWorldTraversableAncestor(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "striatum-lane")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }
	statDaemonSocketACLPath = os.Stat

	parentDir := t.TempDir()
	if err := os.Chmod(parentDir, 0o755); err != nil {
		t.Fatalf("chmod parent dir: %v", err)
	}
	socketDir := filepath.Join(parentDir, "striatum")
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	socketPath := filepath.Join(socketDir, "daemon-go.sock")
	if err := os.WriteFile(socketPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write socket placeholder: %v", err)
	}

	if err := grantDaemonSocketAccessToLaneUser(socketPath); err != nil {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v", err)
	}
	want := []daemonSocketACLCall{
		{op: "set", spec: "u:striatum-lane:--x", path: socketDir},
		{op: "set", spec: "u:striatum-lane:rw-", path: socketPath},
	}
	if !reflect.DeepEqual(daemonSocketACLCalls, want) {
		t.Fatalf("setfacl calls = %#v, want %#v", daemonSocketACLCalls, want)
	}
}

func TestGrantDaemonSocketAccessToLaneUserSurfacesACLFailure(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "striatum-lane")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }
	setDaemonSocketACL = func(spec, path string) error {
		return errors.New("setfacl failed")
	}

	err := grantDaemonSocketAccessToLaneUser("/run/user/1000/striatum/daemon-go.sock")
	if err == nil || !strings.Contains(err.Error(), "setfacl failed") {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v, want setfacl failure", err)
	}
}

func TestGrantDaemonSocketAccessToLaneUserRollsBackPartialGrant(t *testing.T) {
	t.Setenv(daemonLaneOSUserEnv, "striatum-lane")
	restore := stubDaemonSocketACL(t)
	defer restore()
	currentDaemonUser = func() string { return "daemonuser" }
	setCount := 0
	setDaemonSocketACL = func(spec, path string) error {
		setCount++
		if setCount == 2 {
			return errors.New("setfacl failed")
		}
		daemonSocketACLCalls = append(daemonSocketACLCalls, daemonSocketACLCall{op: "set", spec: spec, path: path})
		return nil
	}

	socketPath := filepath.Join(string(filepath.Separator), "run", "user", "1000", "striatum", "daemon-go.sock")
	err := grantDaemonSocketAccessToLaneUser(socketPath)
	if err == nil || !strings.Contains(err.Error(), "setfacl failed") {
		t.Fatalf("grantDaemonSocketAccessToLaneUser() error = %v, want setfacl failure", err)
	}
	want := []daemonSocketACLCall{
		{op: "set", spec: "u:striatum-lane:--x", path: filepath.Join(string(filepath.Separator), "run", "user", "1000")},
		{op: "remove", spec: "u:striatum-lane", path: filepath.Join(string(filepath.Separator), "run", "user", "1000")},
	}
	if !reflect.DeepEqual(daemonSocketACLCalls, want) {
		t.Fatalf("setfacl calls = %#v, want %#v", daemonSocketACLCalls, want)
	}
}

type daemonSocketACLCall struct {
	op   string
	spec string
	path string
}

var daemonSocketACLCalls []daemonSocketACLCall

func stubDaemonSocketACL(t *testing.T) func() {
	t.Helper()
	origLookup := lookupDaemonLaneUser
	origCurrent := currentDaemonUser
	origSet := setDaemonSocketACL
	origRemove := removeDaemonSocketACL
	origStat := statDaemonSocketACLPath
	daemonSocketACLCalls = nil
	lookupDaemonLaneUser = func(name string) (*user.User, error) {
		return &user.User{Username: name}, nil
	}
	currentDaemonUser = func() string { return "daemonuser" }
	setDaemonSocketACL = func(spec, path string) error {
		daemonSocketACLCalls = append(daemonSocketACLCalls, daemonSocketACLCall{op: "set", spec: spec, path: path})
		return nil
	}
	removeDaemonSocketACL = func(spec, path string) error {
		daemonSocketACLCalls = append(daemonSocketACLCalls, daemonSocketACLCall{op: "remove", spec: spec, path: path})
		return nil
	}
	statDaemonSocketACLPath = func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	return func() {
		lookupDaemonLaneUser = origLookup
		currentDaemonUser = origCurrent
		setDaemonSocketACL = origSet
		removeDaemonSocketACL = origRemove
		statDaemonSocketACLPath = origStat
		daemonSocketACLCalls = nil
	}
}
