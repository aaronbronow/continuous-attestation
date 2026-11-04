.PHONY: dev-up dev-down build test demo demo-esphome clean

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

demo-esphome: build
	@echo "--- 1. Producer: Generating 5-block Sonoff S31 (Proprietary OEM -> ESPHome UART -> 3x HA OTA) chain ---"
	./bin/pkaput init-esphome-demo --chain examples/esphome-s31/packablock.yaml
	./bin/pkaput check examples/esphome-s31/packablock.yaml
	
	@echo "--- 2. Producer: Squashing firmware image & attaching Owner's Manual PDF + ESPHome YAML ---"
	./bin/pkaput squash-release \
		--chain examples/esphome-s31/packablock.yaml \
		--layers 4 \
		--output-tag "sonoff-s31-esphome:2026.10.3" \
		--attach "examples/esphome-s31/owners-manual.pdf,examples/esphome-s31/sonoff-s31-esphome.yaml" \
		--storage local
	
	@echo "--- 3. Producer: Publishing DNSLink pointer ---"
	./bin/pkaput publish-dns --domain "layers.example.internal" --provider coredns
	
	@echo "--- 4. Consumer: Running 3-Gate Zero-Trust Verification ---"
	./bin/pkaput preflight-gate \
		--file bin/layers/flattened.tar.gz \
		--domain "layers.example.internal" \
		--policy examples/esphome-s31/packablock.policy.yaml \
		--query-pkg "esphome/esphome" \
		--query-version "2026.10.3"
