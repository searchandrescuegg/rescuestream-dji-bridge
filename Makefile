# Makefile
all: setup hooks

# requires `nvm use --lts` or `nvm use node`
.PHONY: setup
setup:
	npm install -g @commitlint/config-conventional @commitlint/cli


.PHONY: hooks
hooks:
	@git config --local core.hooksPath .githooks/

# certs generates the JWT signing keypair and a self-signed TLS cert for local
# development. Both target directories are gitignored.
.PHONY: certs
certs:
	@mkdir -p docker/keys docker/emqx/certs
	@if [ ! -f docker/keys/jwt-private.pem ]; then \
		openssl genrsa -out docker/keys/jwt-private.pem 2048 2>/dev/null && \
		chmod 600 docker/keys/jwt-private.pem && \
		openssl rsa -in docker/keys/jwt-private.pem -pubout \
			-out docker/keys/jwt-public.pem 2>/dev/null && \
		echo "generated JWT RS256 keypair in docker/keys/"; \
	else echo "docker/keys/jwt-private.pem already exists"; fi
	@if [ ! -f docker/emqx/certs/cert.pem ]; then \
		openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
			-keyout docker/emqx/certs/key.pem -out docker/emqx/certs/cert.pem \
			-subj "/CN=localhost" 2>/dev/null && \
		echo "generated self-signed TLS cert in docker/emqx/certs/"; \
	else echo "docker/emqx/certs/cert.pem already exists"; fi

# integration runs the end-to-end test: the docker-compose stack plus the mock
# RC Plus, asserting that simulated M30T telemetry is ingested.
.PHONY: integration
integration: certs
	./scripts/integration-test.sh
