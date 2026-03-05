# ECS Task Definition Cleanup

Use only these files:
- `deploy/docker-compose.yml`
- `deploy/delete-task-definitions.env`
- `deploy/data/<your-file>.csv`

Compose mounts `deploy/data` as `/data`, and the app reads whatever file is set in `DEFINITIONS_FILE` inside `deploy/delete-task-definitions.env`.
With `WRITE_BACK=true`, it writes statuses into the same `DEFINITIONS_FILE`.
If `DEFINITIONS_FILE` is missing/not found, app treats it as empty and creates it during write-back.

## Run

From project root:

```bash
docker compose -f deploy/docker-compose.yml up
```

If you get `permission denied` for `open /data/...` on EC2/Linux bind mounts, fix folder/file mode from `deploy/`:

```bash
sudo chmod 775 data
sudo chmod 664 data/definitions.csv
```

## Options (`deploy/delete-task-definitions.env`)

- `ACTION=both|deregister|delete`
- `AWS_REGION=...`
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` (optional static creds)
- `DEFINITIONS_FILE=/data/definitions.csv` (or `/data/any-name.csv`)
- `WRITE_BACK=true|false` (default true when `DEFINITIONS_FILE` is set). When `false`, no CSV files are written and results are printed to console.
- `RESULT_FILE=/data/definitions.result.csv`
- `RETRY_FAILED_ONLY=true|false` (process only `*-fail` rows)
- `API_RPS=1` (max mutating ECS calls per second; helps avoid API throttling)
- `DRY_RUN=true|false`

Status values written to file:
- `deregister-success` / `deregister-fail` / `deregister-not-found`
- `delete-success` / `delete-fail` / `delete-not-found`
