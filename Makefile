.PHONY: run build test vet fmt lint vuln migrate-up migrate-down migrate-status seed tidy

run:            ## run the API locally (reads .env)
	go run ./cmd/api

build:          ## build api and migrate binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/migrate ./cmd/migrate

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:           ## requires golangci-lint
	golangci-lint run

vuln:           ## scan dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:   ## roll back one migration
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

seed:           ## create the first super admin from SEED_ADMIN_* in .env
	go run ./cmd/seed

tidy:
	go mod tidy
