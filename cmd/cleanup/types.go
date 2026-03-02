package main

type appConfig struct {
	Region          string
	Profile         string
	DefinitionsFile string
	ResultFile      string
	Action          string
	APIRPS          int
	DryRun          bool
	WriteBack       bool
	RetryFailedOnly bool
}

type definitionRecord struct {
	Identifier       string
	DeregisterStatus string
	DeleteStatus     string
	LastError        string
}

type runStats struct {
	TargetsLoaded      int
	SkippedAlreadyDone int
	Deregistered       int
	DeregisterFailed   int
	DeleteSucceeded    int
	DeleteFailed       int
}

const (
	actionBoth       = "both"
	actionDeregister = "deregister"
	actionDelete     = "delete"

	statusDeregisterSuccess  = "deregister-success"
	statusDeregisterFail     = "deregister-fail"
	statusDeregisterNotFound = "deregister-not-found"
	statusDeleteSuccess      = "delete-success"
	statusDeleteFail         = "delete-fail"
	statusDeleteNotFound     = "delete-not-found"
)
