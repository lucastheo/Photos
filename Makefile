.PHONY: build run run-api run-web test-unit test-integration test-coverage test-perf test-e2e lint lint-max-file check

build:
	go build -o bin/phototracker ./cmd/phototracker
	npm --prefix web run build

run:
	@trap 'trap - INT TERM EXIT; kill 0' INT TERM EXIT; \
	go run ./cmd/phototracker & \
	npm --prefix web run dev & \
	wait

run-api:
	go run ./cmd/phototracker

run-web:
	npm --prefix web run dev

test-unit:
	go test -short -race ./cmd/... ./internal/...
	npm --prefix web run test:unit

test-integration:
	go test -race -run Integration ./cmd/... ./internal/...

test-coverage:
	go test -race -coverpkg=./internal/... -coverprofile=coverage.out -covermode=atomic ./internal/...
	./scripts/check-coverage.sh coverage.out
	npm --prefix web run test:coverage

test-perf:
	go test -bench=. -benchmem -run=^$$ ./internal/...
	@if [ -f e2e/package.json ] && node -e "const p=require('./e2e/package.json'); process.exit(p.scripts && p.scripts['test:perf'] ? 0 : 1)"; then \
		npm --prefix e2e run test:perf; \
	else \
		echo "Skipping E2E perf benchmarks (e2e/ scheduled for Phase 5/6)."; \
	fi

test-e2e:
	@if [ -f e2e/package.json ]; then \
		npm --prefix e2e test; \
	else \
		echo "Skipping E2E tests (e2e/ scheduled for Phase 5)."; \
	fi

lint-max-file:
	./scripts/lint-max-file.sh

lint:
	golangci-lint run
	@diff_out="$$(golangci-lint fmt --diff)" || exit 1; \
	if [ -n "$$diff_out" ]; then \
		printf '%s\n' "$$diff_out"; \
		echo "FAIL: gofumpt formatting differences found. Run 'golangci-lint fmt'." >&2; \
		exit 1; \
	fi
	npm --prefix web run lint
	npm --prefix web run typecheck

check: lint-max-file lint test-coverage test-e2e test-perf
