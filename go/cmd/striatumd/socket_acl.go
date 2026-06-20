package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const daemonLaneOSUserEnv = "STRIATUM_LANE_OS_USER"

var (
	lookupDaemonLaneUser = user.Lookup
	currentDaemonUser    = func() string {
		if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
			return strings.TrimSpace(u.Username)
		}
		return strings.TrimSpace(os.Getenv("USER"))
	}
	setDaemonSocketACL = func(spec, path string) error {
		return exec.Command("setfacl", "-m", spec, path).Run()
	}
	removeDaemonSocketACL = func(spec, path string) error {
		return exec.Command("setfacl", "-x", spec, path).Run()
	}
	statDaemonSocketACLPath = os.Stat
)

func clearDaemonSocketAccessFromLaneUser(socketPath string) {
	laneUser := configuredDaemonLaneUser()
	if laneUser == "" || runtime.GOOS == "windows" {
		return
	}
	clearDaemonSocketACLs(socketPath, laneUser)
}

func grantDaemonSocketAccessToLaneUser(socketPath string) error {
	laneUser := configuredDaemonLaneUser()
	if laneUser == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%s is not supported on windows", daemonLaneOSUserEnv)
	}
	if _, err := lookupDaemonLaneUser(laneUser); err != nil {
		return fmt.Errorf("%s=%s lookup: %w", daemonLaneOSUserEnv, laneUser, err)
	}
	entries := daemonSocketACLGrantTargets(socketPath, laneUser)
	applied := make([]daemonSocketACLTarget, 0, len(entries))
	for _, entry := range entries {
		if err := setDaemonSocketACL(entry.spec, entry.path); err != nil {
			clearAppliedDaemonSocketACLs(applied, laneUser)
			return fmt.Errorf("grant daemon socket ACL %s on %s: %w", entry.spec, entry.path, err)
		}
		applied = append(applied, entry)
	}
	return nil
}

type daemonSocketACLTarget struct {
	path string
	spec string
}

func daemonSocketACLTargets(socketPath, laneUser string) []daemonSocketACLTarget {
	socketPath = filepath.Clean(socketPath)
	socketDir := filepath.Dir(socketPath)
	parentDir := filepath.Dir(socketDir)
	candidates := []daemonSocketACLTarget{
		{path: parentDir, spec: "u:" + laneUser + ":--x"},
		{path: socketDir, spec: "u:" + laneUser + ":--x"},
		{path: socketPath, spec: "u:" + laneUser + ":rw-"},
	}
	entries := make([]daemonSocketACLTarget, 0, len(candidates))
	seen := map[string]bool{}
	for _, entry := range candidates {
		if entry.path == "." || entry.path == "" || seen[entry.path] {
			continue
		}
		seen[entry.path] = true
		entries = append(entries, entry)
	}
	return entries
}

func daemonSocketACLGrantTargets(socketPath, laneUser string) []daemonSocketACLTarget {
	entries := daemonSocketACLTargets(socketPath, laneUser)
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.spec == "u:"+laneUser+":--x" && directoryAlreadyWorldTraversable(entry.path) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func directoryAlreadyWorldTraversable(path string) bool {
	info, err := statDaemonSocketACLPath(path)
	if err != nil || !info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o001 != 0
}

func clearDaemonSocketACLs(socketPath, laneUser string) {
	clearAppliedDaemonSocketACLs(daemonSocketACLTargets(socketPath, laneUser), laneUser)
}

func clearAppliedDaemonSocketACLs(entries []daemonSocketACLTarget, laneUser string) {
	for i := len(entries) - 1; i >= 0; i-- {
		_ = removeDaemonSocketACL("u:"+laneUser, entries[i].path)
	}
}

func configuredDaemonLaneUser() string {
	laneUser := strings.TrimSpace(os.Getenv(daemonLaneOSUserEnv))
	if laneUser == "" {
		return ""
	}
	if daemonUser := currentDaemonUser(); sameDaemonOSUsername(laneUser, daemonUser) {
		return ""
	}
	return laneUser
}

func sameDaemonOSUsername(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasSuffix(b, `\`+a)
}
