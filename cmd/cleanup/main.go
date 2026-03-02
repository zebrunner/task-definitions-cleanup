package main

import (
	"context"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()

	cfg, err := loadConfigFromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("aws config error: %v", err)
	}

	records, err := loadRecords(cfg.DefinitionsFile)
	if err != nil {
		log.Fatalf("definitions load error: %v", err)
	}

	records, err = runCleanup(ctx, cfg, awsCfg, records)
	if err != nil {
		log.Fatalf("cleanup failed: %v", err)
	}

	if cfg.WriteBack && cfg.DefinitionsFile != "" && !cfg.DryRun {
		if err := writeRecords(cfg.DefinitionsFile, records); err != nil {
			log.Fatalf("failed to write definitions file: %v", err)
		}
		log.Printf("definitions file updated: %s", cfg.DefinitionsFile)
	}

	if cfg.WriteBack && cfg.DryRun {
		log.Println("dry-run: write-back skipped")
	}

	_ = os.Stdout.Sync()
}
