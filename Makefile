.PHONY: test test-docker build demo fixture-real

test:
	go vet ./...
	go test ./...

# Mesma suíte, dentro de um container: não depende do Go instalado na máquina.
test-docker:
	docker build --target test -t cicd-test .

build:
	go build -o bin/cicd ./cmd/cicd

# Mostra o gate reprovando e depois liberando com aprovação explícita.
demo: build
	-./bin/cicd plan-gate -plan testdata/plans/replace-sensitive.json -policy config/plan-policy.json
	@echo "--- agora com aprovação explícita ---"
	./bin/cicd plan-gate -plan testdata/plans/replace-sensitive.json -policy config/plan-policy.json -approved

# Regenera o plano real (precisa do Terraform instalado; não usa nuvem).
fixture-real:
	./scripts/gerar-plano-real.sh > testdata/real/replace.json
