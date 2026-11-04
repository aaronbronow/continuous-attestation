.PHONY: dev-up dev-down build test demo clean

dev-up:
	docker compose up -d

dev-down:
	docker compose down -v

build:
	go build -o bin/pkaput ./cmd/pkaput

test:
	go test -v ./...

clean:
	rm -rf bin/ packablock.yaml

demo: build
	@echo "--- 1. Producer (pkablk): Initializing & appending continuous attestation blocks ---"
	pkablk init packablock.yaml -l test/fixtures/package-lock.v1.json
	pkablk append packablock.yaml -l test/fixtures/package-lock.v2.json
	pkablk check packablock.yaml
	
	@echo "--- 2. Producer (pkaput): Squashing layers & putting bundle into IPFS/Storage ---"
	./bin/pkaput squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --storage local
	
	@echo "--- 3. Producer (pkaput): Publishing DNSLink pointer ---"
	./bin/pkaput publish-dns --domain "layers.example.internal" --provider coredns
	
	@echo "--- 4. Consumer (pkaput): Running Zero-Trust Pre-flight Evaluation ---"
	./bin/pkaput preflight-gate --digest $$(cat bin/.latest_digest) --dns-server "127.0.0.1:1053"
