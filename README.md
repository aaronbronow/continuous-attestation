# Continuous Attestation & `pkaput` — Zero-Lock-In Supply Chain Persistence

[![Continuous Attestation & pkaput Zero-Trust Demo](https://github.com/aaronbronow/continuous-attestation/actions/workflows/continuous-attestation.yml/badge.svg)](https://github.com/aaronbronow/continuous-attestation/actions/workflows/continuous-attestation.yml)

**Live Interactive Verifier & Trustless IPFS Gateways:**
- **Cloudflare Worker Edge Gateway:** [`https://continuous-attestation-demo.abronow.workers.dev`](https://continuous-attestation-demo.abronow.workers.dev)
- **GitHub Pages Static IPFS Mirror:** [`https://aaronbronow.github.io/continuous-attestation/`](https://aaronbronow.github.io/continuous-attestation/)

This repository is a live reference implementation and interactive demo showing how **Continuous Attestation** ([`pkablk`](https://github.com/Packablock/packablock-client)) and **`pkaput`** work together to verify every software dependency transition **without vendor lock-in or reliance on a single business-controlled container registry**:

$$\text{1. Build Chain (\texttt{pkablk})} \rightarrow \text{2. Squash \& Put (\texttt{pkaput squash-release})} \rightarrow \text{3. Publish DNSLink (\texttt{pkaput publish-dns})} \rightarrow \text{4. Consumer Gate (\texttt{pkaput preflight-gate})}$$

---

## Why Continuous Attestation + `pkaput`?

Traditional container registries and SaaS SBOM platforms trap supply-chain provenance inside a single vendor's control plane. When intermediate layers are squashed for production or moved across mirrors, historical dependency transitions disappear.

By pairing **`pkablk`** with **`pkaput`**:
1. **Cryptographic Ledger (`packablock.yaml`)**: Every lockfile transition (`package-lock.json`, `bun.lockb`, `yarn.lock`, `go.mod`) is chained via deterministic SHA-256 `data_hash` + `prev_meta_hash` links.
2. **Content-Addressed Persistence (`CARv1` / `IPFS` / `Cloudflare R2` / `Storacha`)**: `pkaput squash-release` flattens OCI layers, generates a CycloneDX v1.5 SBOM, signs an in-toto Statement v1 (`DSSEv1` PAE) + W3C Verifiable Credential (`did:key:z6Mk...` Ed25519) preserving `squashedLineage.precursors`, and packages everything into a deterministic Content-Addressed Archive (`release-bundle.car`).
3. **Registry-Agnostic Discovery (`RFC 4648 Base32 DNSLink`)**: `pkaput publish-dns` maps any squashed OCI layer `sha256:<hex>` digest to `_dnslink.<base32-digest>.<domain>` (`TXT "dnslink=/ipfs/<CID>"`), allowing any consumer to locate the attestation bundle via standard DNS.
4. **Lazy Zero-Trust Pre-Flight Gate (`pkaput preflight-gate`)**: Consumers resolve the DNSLink record, lazily fetch `attestation.dsse.json` (`~2 KB`) before downloading the full SBOM, verify the Ed25519 signature and hash chain, audit exact version upgrade history, and enforce `packablock.policy.yaml`.

---

## 1. Separation of Concerns: `pkablk` vs. `pkaput`

| Tool | Runtime | Core Responsibility |
| :--- | :--- | :--- |
| **`pkablk`** ([`Packablock/packablock-client`](https://github.com/Packablock/packablock-client)) | TypeScript / Bun | **Step 1 — Continuous Attestation Ledger**: Parses real lockfiles (`package-lock.json`, `bun.lockb`, `yarn.lock`), records semantic/sourcemap diffs (`pkablk init`, `pkablk append`, `pkablk rollover`), and verifies the `packablock.yaml` chain (`pkablk check`, `pkablk audit --visualize`). |
| **`pkaput`** (`cmd/pkaput` in this repo) | Go (`go1.24+`) | **Steps 2–4 — Squash, Put & Gatekeep**: Ingests the verified `packablock.yaml`, squashes intermediate OCI layers into a single flattened layer (`sha256:aef8959f...`), generates the CycloneDX SBOM + in-toto DSSE (`squashedLineage.precursors`) + W3C Verifiable Credential, **puts** the UnixFS/CARv1 bundle into persistent storage (`Kubo`, `Cloudflare R2`, or `Storacha`), publishes the Base32 `_dnslink` TXT record (`CoreDNS` or `Cloudflare DNS`), and runs the consumer `preflight-gate`. |

---

## 2. Repository Structure

```text
.
├── .github/workflows/
│   └── continuous-attestation.yml # CI/CD pipeline: runs all 4 stages + deploys interactive verifier
├── cmd/
│   └── pkaput/                    # Unified Go CLI (squash-release, publish-dns, preflight-gate, check, init-esphome-demo)
│       └── main.go
├── examples/
│   └── esphome-s31/               # Featured IoT Scenario: Sonoff S31 (Proprietary OEM -> ESPHome UART -> 3x HA OTA)
│       ├── packablock.yaml        # 5-block signed hardware & firmware attestation chain
│       ├── packablock.policy.yaml # Zero-trust policy (rejects proprietary eWeLink SDK in release)
│       ├── owners-manual.pdf      # Attached factory Owner's Manual & J1 UART pinout specification
│       └── sonoff-s31-esphome.yaml# Attached ESPHome YAML configuration & link to devices.esphome.io/devices/Sonoff-S31
├── internal/
│   ├── attestation/               # in-toto Statement v1, DSSE v1.0 PAE, W3C Verifiable Credential & VSA
│   │   └── attestation.go
│   ├── chain/                     # Parser & verifier for official packablock.yaml + pkablk CLI bridge
│   │   ├── chain.go
│   │   └── chain_test.go
│   ├── dnslink/                   # RFC 4648 Base32 label encoder, CoreDNS zone updater, Cloudflare DNS API v4 & UDP resolver
│   │   ├── cloudflare.go
│   │   └── dnslink.go
│   ├── ipfs/                      # CARv1 encoder, Kubo pinner (:5002), R2/Storacha Edge uploader & Lazy HTTP Gateway fetcher
│   │   ├── edge_store.go
│   │   └── ipfs.go
│   ├── oci/                       # Intermediate layer generator, layer squasher, CycloneDX SBOM & OCI Registry push (:5001)
│   │   └── oci.go
│   └── policy/                    # Gatekeeper policy engine evaluating packablock.policy.yaml (Spec v1 §4.7.2)
│       └── policy.go
├── worker/                        # Cloudflare Worker: Interactive Live Verifier UI + Zero-Egress R2 Trustless IPFS Gateway
│   ├── wrangler.toml
│   └── src/
│       ├── index.ts
│       ├── seed.ts
│       └── ui.ts
├── scripts/
│   └── build-static-demo.mjs      # Generates static verifier & content-addressed /ipfs/<CID>/ tree for GitHub Pages
├── schemas/                       # Normative JSON Schemas
│   ├── chain-block.v1.json
│   └── squash-predicate.v1.json
├── test/
│   ├── coredns/                   # CoreDNS configuration & dynamic zone file for layers.example.internal
│   └── fixtures/                  # Lockfiles (v1/v2), CycloneDX SBOMs, policy rules, and Ed25519 did:key material
├── docker-compose.yml             # Ephemeral local zero-trust stack: Registry (:5001) + Kubo IPFS (:5002/:8080) + CoreDNS (:1053)
├── Makefile                       # End-to-end build, test, and demo orchestration
└── README.md
```

---

## 3. Featured Live Scenario: ITEAD Sonoff S31 Smart Plug (`ESP8266EX` + `CSE7766`) Firmware Liberation & 3x Home Assistant OTA

Both the [Live Web Verifier](https://continuous-attestation-demo.abronow.workers.dev) and `examples/esphome-s31/` model a complete 5-block hardware-to-firmware supply-chain lifecycle for an **ITEAD Sonoff S31 Smart Plug** (`FCC ID: 2APN5S31`, `ESP8266EX` + `CSE7766` energy monitoring IC):

| Block | Event | Firmware / Dependency Transition | Attached Artifact / Provenance Link |
| :--- | :--- | :--- | :--- |
| **Block `#0`** | `factory_oem_baseline` | `itead/ewelink-oem-firmware@2.6.1`, `espressif/esp8266-nonos-sdk@2.2.1` | [`owners-manual.pdf`](examples/esphome-s31/owners-manual.pdf) (`bafkreih2dwc3wrfoli4fvnldafjaoj2zkdphnmdapzff3l52lp6nkvwpqu`) |
| **Block `#1`** | `uart_liberation_flash` | Physical 3.3V FTDI flash on header `J1` (`GPIO0` $\rightarrow$ `GND`) removing proprietary OEM SDK and initializing **ESPHome `2026.7.0`** (3 versions back) | [`sonoff-s31-esphome.yaml`](examples/esphome-s31/sonoff-s31-esphome.yaml) (`bafkreibbzyj5gy3yrcuxyzhrs3yiv6352j4oali53dqstiij5x6e3c3vga`) + [`devices.esphome.io/devices/Sonoff-S31`](https://devices.esphome.io/devices/Sonoff-S31) |
| **Block `#2`** | `ha_esphome_ota_update` (`#1`) | Home Assistant ESPHome OTA #1: `esphome/esphome` `2026.7.0` $\rightarrow$ `2026.8.2`, `lwip/lwip-esp8266` `2.1.2` $\rightarrow$ `2.1.3-patch1` (Wi-Fi beacon stability) | Signed `ed25519` block `730b6ba2ca5e...` |
| **Block `#3`** | `ha_esphome_ota_update` (`#2`) | Home Assistant ESPHome OTA #2: `esphome/esphome` `2026.8.2` $\rightarrow$ `2026.9.1`, `esphome/ESPAsyncWebServer` `3.1.0` $\rightarrow$ `3.3.2` (**remediates `CVE-2026-4108`**) | Signed `ed25519` block `dfef64f18ba5...` |
| **Block `#4`** | `ha_esphome_ota_update` (`#3`) | Home Assistant ESPHome OTA #3 (Present Day): `esphome/esphome` `2026.9.1` $\rightarrow$ `2026.10.3`, `esphome/noise-c` `0.1.9` $\rightarrow$ `0.1.10` (Noise API key rotation) | Terminal `meta_hash`: `72da938cfb25373a289f4f9c67d868d6224d4602debd509ada66da087c34daa2` |

### Run the Sonoff S31 ESPHome Scenario via CLI
```powershell
# 1. Generate & verify the 5-block Sonoff S31 chain
./bin/pkaput.exe init-esphome-demo --chain examples/esphome-s31/packablock.yaml
./bin/pkaput.exe check examples/esphome-s31/packablock.yaml

# 2. Squash the 4 ESPHome firmware layers & attach the Owner's Manual PDF + ESPHome YAML into CARv1
./bin/pkaput.exe squash-release `
  --chain examples/esphome-s31/packablock.yaml `
  --layers 4 `
  --output-tag "sonoff-s31-esphome:2026.10.3" `
  --attach "examples/esphome-s31/owners-manual.pdf,examples/esphome-s31/sonoff-s31-esphome.yaml" `
  --storage local

# 3. Publish the RFC 4648 Base32 DNSLink record
./bin/pkaput.exe publish-dns --domain "layers.example.internal" --provider coredns

# 4. Run the Consumer 3-Gate Zero-Trust Pre-Flight Verification
./bin/pkaput.exe preflight-gate `
  --file bin/layers/flattened.tar.gz `
  --domain "layers.example.internal" `
  --policy examples/esphome-s31/packablock.policy.yaml `
  --query-pkg "esphome/esphome" `
  --query-version "2026.10.3"
```

---

## 4. Quickstart & End-to-End Local Demo (`secure-lib:v1.0.0`)

### Build `pkaput` & Run Unit Tests
```powershell
go build -o bin/pkaput.exe ./cmd/pkaput
go test -v ./...
```

### Execute the 4-Stage Producer-to-Consumer Workflow (`pkablk` $\rightarrow$ `pkaput`)
```powershell
# 1. Producer: Verify packablock.yaml chain & squash intermediate layers into CARv1 + IPFS bundle
./bin/pkaput.exe squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --storage local

# 2. Producer: Publish RFC 4648 Base32 DNSLink TXT pointer to CoreDNS (or --provider cloudflare)
./bin/pkaput.exe publish-dns --domain "layers.example.internal" --provider coredns

# 3. Consumer: Run Zero-Trust Pre-Flight Gate against the release layer tarball (or against a live HTTP gateway)
./bin/pkaput.exe preflight-gate --file bin/layers/flattened.tar.gz --domain "layers.example.internal" --dns-server "127.0.0.1:1053"
```

### Optional: Start Local Containerized Stack (`registry:2`, `ipfs/kubo`, `coredns`)
```powershell
docker compose up -d
```

---

## 4. Persistent Edge Storage Mode (`Cloudflare Worker + R2 + Cloudflare DNS`)

```powershell
# Deploy the Interactive Verifier & Trustless IPFS Gateway Worker
cd worker
npx wrangler deploy

# Put release bundle to R2 + Storacha and publish DNSLink to Cloudflare DNS
$env:PKAPUT_WORKER_URL = "https://continuous-attestation-demo.abronow.workers.dev"
$env:PKAPUT_WORKER_TOKEN = "<your-worker-put-token>"
$env:CF_ZONE_ID = "<your-cloudflare-zone-id>"
$env:CF_API_TOKEN = "<your-cloudflare-dns-token>"

./bin/pkaput.exe squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --storage r2
./bin/pkaput.exe publish-dns --domain "layers.packablock.com" --provider cloudflare
./bin/pkaput.exe preflight-gate --file bin/layers/flattened.tar.gz --domain "layers.packablock.com" --ipfs-gateway "$env:PKAPUT_WORKER_URL,https://aaronbronow.github.io/continuous-attestation,https://w3s.link"
```
