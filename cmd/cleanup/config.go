package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func loadConfigFromEnv() (*appConfig, error) {
	cfg := &appConfig{
		Region:          getEnvDefault("AWS_REGION", "us-east-1"),
		Profile:         strings.TrimSpace(os.Getenv("AWS_PROFILE")),
		DefinitionsFile: strings.TrimSpace(os.Getenv("DEFINITIONS_FILE")),
		Action:          strings.ToLower(getEnvDefault("ACTION", actionBoth)),
	}
	apiRPS, err := parseIntEnv("API_RPS", 3)
	if err != nil {
		return nil, err
	}
	if apiRPS <= 0 {
		return nil, errors.New("API_RPS must be > 0")
	}
	cfg.APIRPS = apiRPS

	dryRun, err := parseBoolEnv("DRY_RUN", false)
	if err != nil {
		return nil, err
	}
	cfg.DryRun = dryRun

	writeBackDefault := cfg.DefinitionsFile != ""
	writeBack, err := parseBoolEnv("WRITE_BACK", writeBackDefault)
	if err != nil {
		return nil, err
	}
	cfg.WriteBack = writeBack

	retryFailedOnly, err := parseBoolEnv("RETRY_FAILED_ONLY", false)
	if err != nil {
		return nil, err
	}
	cfg.RetryFailedOnly = retryFailedOnly

	cfg.ResultFile = strings.TrimSpace(os.Getenv("RESULT_FILE"))
	if cfg.ResultFile == "" {
		if cfg.DefinitionsFile != "" {
			cfg.ResultFile = filepath.Join(filepath.Dir(cfg.DefinitionsFile), "definitions.result.csv")
		} else {
			cfg.ResultFile = "/data/definitions.result.csv"
		}
	}

	if cfg.DefinitionsFile == "" {
		return nil, errors.New("set DEFINITIONS_FILE")
	}
	switch cfg.Action {
	case actionBoth, actionDeregister, actionDelete:
	default:
		return nil, errors.New("ACTION must be one of: both, deregister, delete")
	}
	if cfg.WriteBack && cfg.DefinitionsFile == "" {
		return nil, errors.New("WRITE_BACK=true requires DEFINITIONS_FILE")
	}
	return cfg, nil
}

func parseBoolEnv(name string, defaultValue bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return defaultValue, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s should be true/false: %w", name, err)
	}
	return b, nil
}

func parseIntEnv(name string, defaultValue int) (int, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return defaultValue, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s should be integer: %w", name, err)
	}
	return n, nil
}

func getEnvDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func blankAsDefault(v string) string {
	if strings.TrimSpace(v) == "" {
		return "<empty>"
	}
	return v
}
