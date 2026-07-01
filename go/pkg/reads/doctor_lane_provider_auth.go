package reads

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/laneproviderauth"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

var (
	doctorLaneProviderAuthCheck = laneproviderauth.Check
	lookupOSUserHome            = func(name string) (string, bool) {
		u, err := user.Lookup(name)
		if err != nil || strings.TrimSpace(u.HomeDir) == "" {
			return "", false
		}
		return strings.TrimSpace(u.HomeDir), true
	}
)

func HandleDoctorLaneProviderAuth(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	provider := strings.ToLower(strings.TrimSpace(stringParam(envelope, "lane_provider_auth")))
	timeout, err := doctorLaneProviderAuthTimeout(envelope.Params["timeout"])
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(stringParam(envelope, "run_id"))
	laneID := strings.TrimSpace(stringParam(envelope, "lane_id"))
	repositoryID := strings.TrimSpace(stringParam(envelope, "repository_id"))
	laneConfig, err := doctorLaneProviderConfig(ctx, runner, repositoryID, runID, laneID, provider)
	if err != nil {
		return nil, err
	}
	runAsUser := doctorLaneProviderRunAsUser()
	env := os.Environ()
	if runAsUser != "" {
		env = doctorLaneProviderRunAsEnv(runAsUser)
	}
	result := doctorLaneProviderAuthCheck(ctx, laneproviderauth.Params{
		Provider:  provider,
		Binary:    laneConfig.Binary,
		RunID:     runID,
		LaneID:    laneID,
		RunAsUser: runAsUser,
		Env:       laneproviderauth.SanitizeEnv(env, laneConfig.PathPrefix),
		Timeout:   timeout,
	})
	return result.ToMap(), nil
}

func doctorLaneProviderRunAsEnv(runAsUser string) []string {
	env := doctorLaneProviderRunAsPassThrough(os.Environ())
	env = append(env, "PATH="+os.Getenv("PATH"), "USER="+runAsUser, "LOGNAME="+runAsUser)
	if home, ok := lookupOSUserHome(runAsUser); ok {
		env = append(env, "HOME="+home)
	}
	return env
}

func doctorLaneProviderRunAsPassThrough(base []string) []string {
	out := []string{}
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		switch key {
		case "TERM", "COLORTERM", "LANG", "LANGUAGE", "TZ":
			out = append(out, entry)
		default:
			if strings.HasPrefix(key, "LC_") {
				out = append(out, entry)
			}
		}
	}
	return out
}

type doctorLaneProviderAuthLaneConfig struct {
	Binary     string
	PathPrefix []string
}

func doctorLaneProviderConfig(ctx context.Context, runner db.Runner, repositoryID, runID, laneID, provider string) (doctorLaneProviderAuthLaneConfig, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = laneproviderauth.ProviderCodex
	}
	config := doctorLaneProviderAuthLaneConfig{Binary: provider}
	if repositoryID == "" || runID == "" || laneID == "" {
		return config, nil
	}
	rows, err := collectRows(ctx, runner, `
		SELECT ws.workflow_json
		  FROM striatumd.runs r
		  JOIN striatumd.workflow_snapshots ws
		    ON ws.repository_id = r.repository_id AND ws.workflow_snapshot_id = r.workflow_snapshot_id
		 WHERE r.repository_id = $1 AND r.run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return config, err
	}
	if len(rows) == 0 {
		return config, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	workflow := objectFromJSONish(rows[0]["workflow_json"])
	lane := objectFromJSONish(objectFromJSONish(workflow["lanes"])[laneID])
	if len(lane) == 0 {
		return config, rpc.NewError("not_found", fmt.Sprintf("lane %s not found in run %s workflow snapshot", laneID, runID), nil)
	}
	if command := stringListJSONish(lane["command"]); len(command) > 0 && agentloop.LaneAdapterName(command[0]) == provider {
		config.Binary = command[0]
	}
	config.PathPrefix = cleanPathPrefix(stringListJSONish(lane["path_prefix"]))
	return config, nil
}

func doctorLaneProviderAuthTimeout(raw any) (time.Duration, error) {
	if raw == nil {
		return laneproviderauth.DefaultTimeout, nil
	}
	switch value := raw.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return 0, rpc.NewError("schema_invalid", "doctor lane-provider-auth timeout must be positive", nil)
		}
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			return parsed, nil
		}
		seconds, err := strconv.Atoi(value)
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second, nil
		}
	case int:
		if value > 0 {
			return time.Duration(value) * time.Second, nil
		}
	case int64:
		if value > 0 {
			return time.Duration(value) * time.Second, nil
		}
	case float64:
		if value > 0 && value == float64(int(value)) {
			return time.Duration(int(value)) * time.Second, nil
		}
	}
	return 0, rpc.NewError("schema_invalid", "doctor lane-provider-auth timeout must be a positive duration or seconds value", nil)
}

func doctorLaneProviderRunAsUser() string {
	laneUser := strings.TrimSpace(os.Getenv(laneOSUserEnv))
	if laneUser == "" {
		return ""
	}
	daemonUser := currentUsername()
	if daemonUser != "" && sameDoctorOSUsername(laneUser, daemonUser) {
		return ""
	}
	return laneUser
}

func sameDoctorOSUsername(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasSuffix(b, `\`+a)
}

func stringListJSONish(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}

func cleanPathPrefix(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		out = append(out, filepath.Clean(path))
	}
	return out
}
