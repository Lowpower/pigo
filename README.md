# pigo

A minimal Go web service.

## Requirements

- Go 1.22+

## Run

```bash
go run .
```

The server listens on `:8080` by default. Override with the `PIGO_ADDR` environment variable (e.g. `PIGO_ADDR=:9000 go run .`).

## Endpoints

| Method | Path      | Description                          |
| ------ | --------- | ------------------------------------ |
| GET    | `/`       | HTML landing page                    |
| GET    | `/health` | JSON health check (`{"status":"ok"}`)|

## Build

```bash
go build ./...
```

## Test

```bash
go test ./...
```
