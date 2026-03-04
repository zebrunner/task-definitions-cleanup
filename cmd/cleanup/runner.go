package main

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

const maxDeleteBatch = 10
const maxThrottleRetries = 6

func runCleanup(ctx context.Context, cfg *appConfig, awsCfg aws.Config, records []*definitionRecord) ([]*definitionRecord, error) {
	client := ecs.NewFromConfig(awsCfg)
	stats := &runStats{}
	start := time.Now()
	callLimiter := newCallLimiter(cfg.APIRPS)

	stats.TargetsLoaded = len(records)
	if len(records) == 0 {
		log.Println("nothing to process (empty file)")
		return records, nil
	}

	log.Println("=== ECS task definition cleanup ===")
	log.Printf("region:              %s", cfg.Region)
	log.Printf("aws profile:         %s", blankAsDefault(cfg.Profile))
	log.Printf("definitions file:    %s", blankAsDefault(cfg.DefinitionsFile))
	log.Printf("action:              %s", cfg.Action)
	log.Printf("api rps limit:       %d", cfg.APIRPS)
	log.Printf("aws retry:           standard (max attempts=5)")
	log.Printf("dry run:             %t", cfg.DryRun)
	log.Printf("write back:          %t", cfg.WriteBack)
	log.Printf("retry failed only:   %t", cfg.RetryFailedOnly)
	log.Printf("result file:         %s", blankAsDefault(cfg.ResultFile))
	log.Printf("targets loaded:      %d", stats.TargetsLoaded)

	deleteOrder := make([]string, 0)
	deleteToRecord := map[string]*definitionRecord{}

	for i, rec := range records {
		needDeregister := cfg.Action != actionDelete && shouldProcessStatus(rec.DeregisterStatus, cfg.RetryFailedOnly)
		needDelete := cfg.Action != actionDeregister && shouldProcessStatus(rec.DeleteStatus, cfg.RetryFailedOnly)
		if !needDeregister && !needDelete {
			stats.SkippedAlreadyDone++
			continue
		}

		log.Printf("[%d/%d] processing %s", i+1, len(records), rec.Identifier)
		rec.LastError = ""

		if needDeregister {
			if cfg.DryRun {
				rec.DeregisterStatus = statusDeregisterSuccess
				log.Printf("  dry-run: would deregister")
			} else {
				err := invokeWithRateLimitAndRetry(ctx, callLimiter, "DeregisterTaskDefinition", func() error {
					_, callErr := client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
						TaskDefinition: aws.String(rec.Identifier),
					})
					return callErr
				})
				if err != nil {
					if isNotFound(err) {
						rec.DeregisterStatus = statusDeregisterNotFound
						log.Printf("  not found, marking as not-found")
					} else if isDeregisterAlreadyHandledMessage(err.Error()) {
						rec.DeregisterStatus = statusDeregisterSuccess
						log.Printf("  already handled for deregister, marking as success")
					} else {
						stats.DeregisterFailed++
						rec.DeregisterStatus = statusDeregisterFail
						rec.LastError = err.Error()
						log.Printf("  deregister failed: %v", err)
						if err := persistProgress(cfg, records); err != nil {
							return nil, err
						}
						continue
					}
				} else {
					stats.Deregistered++
					rec.DeregisterStatus = statusDeregisterSuccess
					log.Printf("  deregistered")
				}
			}
			if err := persistProgress(cfg, records); err != nil {
				return nil, err
			}
		}

		if cfg.Action == actionDeregister || !needDelete {
			if err := persistProgress(cfg, records); err != nil {
				return nil, err
			}
			continue
		}
		deleteToRecord[rec.Identifier] = rec
		deleteOrder = append(deleteOrder, rec.Identifier)
	}

	if cfg.Action != actionDeregister {
		sort.Strings(deleteOrder)
		for i := 0; i < len(deleteOrder); i += maxDeleteBatch {
			end := i + maxDeleteBatch
			if end > len(deleteOrder) {
				end = len(deleteOrder)
			}
			batch := deleteOrder[i:end]
			log.Printf("deleting batch %d..%d", i+1, end)

			if cfg.DryRun {
				for _, id := range batch {
					deleteToRecord[id].DeleteStatus = statusDeleteSuccess
					deleteToRecord[id].LastError = ""
					if err := persistProgress(cfg, records); err != nil {
						return nil, err
					}
				}
				stats.DeleteSucceeded += len(batch)
				continue
			}

			var out *ecs.DeleteTaskDefinitionsOutput
			err := invokeWithRateLimitAndRetry(ctx, callLimiter, "DeleteTaskDefinitions", func() error {
				var callErr error
				out, callErr = client.DeleteTaskDefinitions(ctx, &ecs.DeleteTaskDefinitionsInput{TaskDefinitions: batch})
				return callErr
			})
			if err != nil {
				stats.DeleteFailed += len(batch)
				for _, arn := range batch {
					deleteToRecord[arn].DeleteStatus = statusDeleteFail
					deleteToRecord[arn].LastError = err.Error()
					if err := persistProgress(cfg, records); err != nil {
						return nil, err
					}
				}
				log.Printf("  delete batch failed: %v", err)
				continue
			}

			failedByID := map[string]string{}
			for _, f := range out.Failures {
				target := aws.ToString(f.Arn)
				id := matchFailureToIdentifier(target, batch)
				if id == "" {
					continue
				}
				failedByID[id] = strings.TrimSpace(aws.ToString(f.Reason) + " " + aws.ToString(f.Detail))
			}

			for _, id := range batch {
				rec := deleteToRecord[id]
				msg, failed := failedByID[id]
				if !failed {
					rec.DeleteStatus = statusDeleteSuccess
					rec.LastError = ""
					stats.DeleteSucceeded++
					continue
				}

				switch {
				case isNotFoundMessage(msg):
					rec.DeleteStatus = statusDeleteNotFound
					rec.LastError = ""
					log.Printf("  not found on delete, marking as not-found")
				case isDeleteInProgressMessage(msg):
					rec.DeleteStatus = statusDeleteSuccess
					rec.LastError = ""
					stats.DeleteSucceeded++
				case isActiveDeleteMessage(msg):
					stats.DeleteFailed++
					rec.DeleteStatus = statusDeleteFail
					rec.LastError = msg
				default:
					stats.DeleteFailed++
					rec.DeleteStatus = statusDeleteFail
					rec.LastError = msg
				}
				if err := persistProgress(cfg, records); err != nil {
					return nil, err
				}
			}
		}
	}

	printSummary(start, stats)
	if cfg.WriteBack {
		if err := writeResultRecords(cfg.ResultFile, records); err != nil {
			return nil, err
		}
	} else {
		logResultRecords(records)
	}
	return records, nil
}

func logResultRecords(records []*definitionRecord) {
	log.Println("")
	log.Println("=== Results (write_back=false) ===")
	log.Println("Identifier,deregistered,deleted,error")
	for _, rec := range records {
		log.Printf("%s,%s,%s,%s", rec.Identifier, rec.DeregisterStatus, rec.DeleteStatus, rec.LastError)
	}
}

func shouldProcessStatus(status string, retryFailedOnly bool) bool {
	s := status
	if s == "" {
		return !retryFailedOnly
	}
	if retryFailedOnly {
		return s == statusDeregisterFail || s == statusDeleteFail
	}
	return s != statusDeregisterSuccess &&
		s != statusDeleteSuccess &&
		s != statusDeregisterNotFound &&
		s != statusDeleteNotFound
}

func persistProgress(cfg *appConfig, records []*definitionRecord) error {
	if !cfg.WriteBack || cfg.DryRun || cfg.DefinitionsFile == "" {
		return nil
	}
	if err := writeRecords(cfg.DefinitionsFile, records); err != nil {
		return err
	}
	return writeResultRecords(cfg.ResultFile, records)
}

func printSummary(start time.Time, stats *runStats) {
	elapsed := time.Since(start).Round(time.Second)
	log.Println("")
	log.Println("=== Summary ===")
	log.Printf("targets loaded:            %d", stats.TargetsLoaded)
	log.Printf("already processed/skipped: %d", stats.SkippedAlreadyDone)
	log.Printf("deregistered now:          %d", stats.Deregistered)
	log.Printf("deregister failed:         %d", stats.DeregisterFailed)
	log.Printf("delete succeeded:          %d", stats.DeleteSucceeded)
	log.Printf("delete failed:             %d", stats.DeleteFailed)
	log.Printf("elapsed:                   %s", elapsed)
}

func matchFailureToIdentifier(target string, batch []string) string {
	for _, id := range batch {
		if target == id || strings.HasSuffix(target, id) {
			return id
		}
	}
	return ""
}

func isNotFoundMessage(msg string) bool {
	s := strings.ToLower(msg)
	return strings.Contains(s, "not found") || strings.Contains(s, "does not exist")
}

func isDeleteInProgressMessage(msg string) bool {
	s := strings.ToLower(msg)
	return strings.Contains(s, "delete_in_progress") || strings.Contains(s, "delete in progress")
}

func isActiveDeleteMessage(msg string) bool {
	s := strings.ToLower(msg)
	return strings.Contains(s, "active")
}

func isDeregisterAlreadyHandledMessage(msg string) bool {
	s := strings.ToLower(msg)
	return strings.Contains(s, "already been deregistered") ||
		strings.Contains(s, "already deregistered") ||
		strings.Contains(s, "is inactive") ||
		strings.Contains(s, "not active") ||
		strings.Contains(s, "in the process of being deleted") ||
		strings.Contains(s, "delete_in_progress")
}

func newCallLimiter(rps int) func(context.Context) error {
	if rps <= 0 {
		rps = 1
	}
	interval := time.Second / time.Duration(rps)
	nextAllowed := time.Now()
	return func(ctx context.Context) error {
		now := time.Now()
		if now.Before(nextAllowed) {
			wait := nextAllowed.Sub(now)
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		nextAllowed = time.Now().Add(interval)
		return nil
	}
}

func invokeWithRateLimitAndRetry(ctx context.Context, waitTurn func(context.Context) error, op string, fn func() error) error {
	backoff := 300 * time.Millisecond
	var err error
	for attempt := 0; attempt <= maxThrottleRetries; attempt++ {
		if rateErr := waitTurn(ctx); rateErr != nil {
			return rateErr
		}
		err = fn()
		if err == nil {
			return nil
		}
		if !isThrottling(err) || attempt == maxThrottleRetries {
			return err
		}
		log.Printf("  throttled on %s, retry in %s (%d/%d)", op, backoff, attempt+1, maxThrottleRetries)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	return err
}
