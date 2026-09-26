# Heliostat development tasks. Run `make help` for a list.

# The release version lives in one place: appVersion in charts/heliostat/Chart.yaml.
VERSION   ?= $(shell awk -F'"' '/^appVersion:/ {print $$2}' charts/heliostat/Chart.yaml)
IMAGE     ?= heliostat:$(VERSION)
NAMESPACE ?= heliostat
PLATFORMS ?= linux/amd64,linux/arm64
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

##@ Build

.PHONY: ui
ui: ## Build the web UI and stage it for embedding (ui/dist)
	cd ui && npm ci && npm run build
	find ui/dist -mindepth 1 ! -name README.md -exec rm -rf {} +
	cp -R ui/out/. ui/dist/

.PHONY: diagram
diagram: ## Regenerate the README architecture diagram from the home page component
	cd ui && npm run diagram

.PHONY: build
build: ui ## Build the heliostat binary with the UI embedded (bin/heliostat)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/heliostat ./cmd/heliostat

.PHONY: image
image: ## Build the container image for the local platform
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

.PHONY: image-push
image-push: ## Build and push a multi-arch image (set IMAGE=<registry>/heliostat:<tag>)
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(VERSION) -t $(IMAGE) --push .

##@ Develop

.PHONY: run
run: ## Run the backend on :8080 against your current kubeconfig (config/heliostat.yaml)
	HELIOSTAT_LISTEN=127.0.0.1:8080 go run ./cmd/heliostat

.PHONY: ui-dev
ui-dev: ## Run the UI with hot reload on :3000, proxying API calls to `make run`
	cd ui && npm run dev

##@ Verify

.PHONY: test
test: ## Run Go and UI unit tests
	go test ./cmd/... ./internal/... ./ui
	cd ui && npm test

.PHONY: lint
lint: ## Check formatting, vet, and UI lint and types
	@test -z "$$(gofmt -l cmd internal ui/embed.go)" || (echo "gofmt needed:"; gofmt -l cmd internal ui/embed.go; exit 1)
	go vet ./cmd/... ./internal/... ./ui
	cd ui && npm run format:check && npm run lint && npm run typecheck

.PHONY: chart-verify
chart-verify: ## Lint and render the Helm chart
	helm lint charts/heliostat
	helm template heliostat charts/heliostat --namespace $(NAMESPACE) >/dev/null

.PHONY: verify
verify: lint test chart-verify ## Everything CI runs

##@ Deploy

.PHONY: install
install: ## Install or upgrade the chart (set IMAGE=<registry>/heliostat:<tag>)
	helm upgrade --install heliostat ./charts/heliostat \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=$(word 1,$(subst :, ,$(IMAGE))) \
		--set image.tag=$(word 2,$(subst :, ,$(IMAGE)))

.PHONY: port-forward
port-forward: ## Open the deployed UI on http://127.0.0.1:8080
	kubectl -n $(NAMESPACE) port-forward svc/heliostat-heliostat 8080:80

.PHONY: clean
clean: ## Remove build output
	rm -rf bin ui/out ui/.next
	find ui/dist -mindepth 1 ! -name README.md -exec rm -rf {} +
