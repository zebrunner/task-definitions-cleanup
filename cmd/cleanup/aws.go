package main

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func loadAWSConfig(ctx context.Context, cfg *appConfig) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
		config.WithRetryMode(aws.RetryModeStandard),
		config.WithRetryMaxAttempts(5),
	}

	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}

	return config.LoadDefaultConfig(ctx, opts...)
}

func isNotFound(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "unable to describe task definition")
}

func isThrottling(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "throttl") ||
		strings.Contains(s, "rate exceeded") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "requestlimitexceeded")
}
