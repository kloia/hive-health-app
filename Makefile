CHART        := helm/hive
ENV          ?= dev
VALUES       := helm/envs/$(ENV)/values.yaml
RELEASE      ?= hive
NAMESPACE    ?= hive-$(ENV)
# No default on purpose: every deploy names its target cluster explicitly.
KUBE_CONTEXT ?=

.PHONY: helm-lint helm-template deploy uninstall

helm-lint:
	@for env in helm/envs/*/; do \
		echo "==> $$env"; \
		helm lint $(CHART) -f $$env/values.yaml || exit 1; \
	done

helm-template: check-env
	helm template $(RELEASE) $(CHART) -n $(NAMESPACE) -f $(VALUES)

deploy: check-env check-context
	helm upgrade --install $(RELEASE) $(CHART) \
		--kube-context $(KUBE_CONTEXT) -n $(NAMESPACE) --create-namespace \
		-f $(VALUES) $(HELM_ARGS) \
		--atomic --wait --timeout 5m

uninstall: check-context
	helm uninstall $(RELEASE) --kube-context $(KUBE_CONTEXT) -n $(NAMESPACE)

.PHONY: check-env check-context
check-env:
	@test -f $(VALUES) || { echo "unknown ENV '$(ENV)': $(VALUES) not found"; exit 1; }

check-context:
	@test -n "$(KUBE_CONTEXT)" || { echo "KUBE_CONTEXT is required, e.g. make deploy ENV=$(ENV) KUBE_CONTEXT=my-cluster"; exit 1; }
