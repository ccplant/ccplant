.PHONY: backend-build backend-test frontend-install frontend-test frontend-build docs-install docs-dev docs-build chart-deps chart-test schedule-test webhook-test slackbot-test session-manager-test test

HELM ?= helm

backend-build:
	$(MAKE) -C backend build

backend-test:
	cd backend && go test ./...

frontend-install:
	cd frontend && bun install --frozen-lockfile

frontend-test:
	cd frontend && bun run type-check && bun run test

frontend-build:
	cd frontend && bun run build

docs-install:
	cd docs && bun install --frozen-lockfile

docs-dev:
	cd docs && bun run dev

docs-build:
	cd docs && bun run build

chart-deps:
	$(HELM) dependency build backend/helm/agentapi-proxy
	$(HELM) dependency build chart/ccplant

chart-test: chart-deps
	$(HELM) lint backend/helm/agentapi-proxy --strict
	$(HELM) template backend backend/helm/agentapi-proxy >/dev/null
	$(HELM) lint frontend/helm/agentapi-ui --strict
	$(HELM) template frontend frontend/helm/agentapi-ui >/dev/null
	$(HELM) lint chart/ccplant --strict
	$(HELM) template ccplant chart/ccplant >/dev/null
	HELM=$(HELM) ./scripts/test-helm-render.sh

# Deterministic vertical contract for schedule creation and execution. Repeating
# the backend scenario catches shared-state and ordering regressions without
# retrying failures.
schedule-test:
	cd backend && CGO_ENABLED=1 go test -race -run '^TestScheduleAcceptance_' -count=20 ./internal/modules/schedule
	cd frontend && bun run test -- src/app/components/__tests__/ScheduleListView.test.tsx

webhook-test:
	cd backend && CGO_ENABLED=1 go test -race -count=3 ./internal/modules/webhook/...
	cd frontend && bun run test -- src/app/components/__tests__/WebhookListView.test.tsx

slackbot-test:
	cd backend && CGO_ENABLED=1 go test -race -count=3 ./internal/modules/slackbot/...
	cd frontend && bun run test -- src/app/components/__tests__/SlackbotListView.test.tsx

session-manager-test:
	cd backend && CGO_ENABLED=1 go test -race -count=10 ./internal/modules/sessionmanager/...
	cd frontend && bun run test -- src/components/settings/__tests__/ExternalSessionManagerList.test.tsx

test: backend-test frontend-test chart-test
