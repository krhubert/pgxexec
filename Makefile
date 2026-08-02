.DEFAULT_GOAL := help

.PHONY: help
help: ALIGN=16
help: ## print this help
	@echo ''
	@for file in $(MAKEFILE_LIST); do \
		if grep -q "::* *##" $$file; then \
			awk -F '::? .*## ' -- "/^[^':]+::? .*## /"' { printf "'$$(tput bold)'%-$(ALIGN)s'$$(tput sgr0)' %s\n", $$1, $$2  }' $$file; \
			echo ''; \
		fi; \
	done

.PHONY: mise/install
mise/install: ## Installs tools from mise config file
	mise install

.PHONY: mise/trust
mise/trust: ## Trusts mise config file
	mise trust

.PHONY: sqlc/gen
sqlc/gen: ## Generate sqlc code
	sqlc generate -f ./internal/sqlc/sqlc.yaml

.PHONY: go/test
go/test: ## run go tests
	go test ./...

.PHONY: docker/stop
docker/stop: ## Stop docker postgres container
	@docker rm -f postgres-pgxexec-test 2>/dev/null || true

.PHONY: docker/run
docker/run: ## Run docker postgres container for test
	$(MAKE) docker/stop
	docker run --rm -d \
		--name postgres-pgxexec-test \
		-e POSTGRES_PASSWORD=postgres \
		-p 5432:5432 \
		--health-cmd="pg_isready -U postgres" \
		--health-interval=1s \
		--health-timeout=2s \
		--health-retries=10 \
		postgres:15.3-alpine

.PHONY: docker/wait
docker/wait: ## Wait for database to be ready and schema applied
	@echo "Waiting for database to initialize and apply schema..."
	@until docker exec postgres-pgxexec-test pg_isready -U postgres; do \
		sleep 1; \
	done
	@echo "Database is ready!"

.PHONY: test
test: ## setup and run tests
	$(MAKE) docker/run
	$(MAKE) docker/wait
	@trap '$(MAKE) docker/stop' EXIT; \
	go test ./...
