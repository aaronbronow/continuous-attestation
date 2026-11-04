# Continuous Attestation & `pkaput` — Zero-Lock-In Supply Chain Persistence

[![Continuous Attestation & pkaput Zero-Trust Demo](https://github.com/aaronbronow/continuous-attestation/actions/workflows/continuous-attestation.yml/badge.svg)](https://github.com/aaronbronow/continuous-attestation/actions/workflows/continuous-attestation.yml)

**Live Interactive Verifier & Trustless IPFS Gateways:**
- **Primary Live Demo & Edge Gateway:** [`https://examples.packablock.com`](https://examples.packablock.com)
- **Cloudflare Worker Mirror:** [`https://continuous-attestation-demo.abronow.workers.dev`](https://continuous-attestation-demo.abronow.workers.dev)
- **GitHub Pages Static IPFS Mirror:** [`https://aaronbronow.github.io/continuous-attestation/`](https://aaronbronow.github.io/continuous-attestation/)

This repository is a live reference implementation and interactive demo showing how **Continuous Attestation** ([`pkablk`](https://github.com/Packablock/packablock-client)) and **`pkaput`** work together to verify every software dependency transition **without vendor lock-in or reliance on a single business-controlled container registry**:

> **1. Build Chain (`pkablk`) → 2. Squash & Put (`pkaput squash-release`) → 3. Publish DNSLink (`pkaput publish-dns`) → 4. Consumer Gate (`pkaput preflight-gate`)**

---

## Why Continuous Attestation + `pkaput`?

Traditional container registries and SaaS SBOM platforms trap supply-chain provenance inside a single vendor's control plane. When intermediate layers are squashed for production or moved across mirrors, historical dependency transitions disappear.

By pairing **`pkablk`** with **`pkaput`**:
1. **Cryptographic Ledger (`packablock.yaml`)**: Every lockfile transition (`package-lock.json`, `bun.lockb`, `yarn.lock`, `go.mod`) is chained via deterministic SHA-256 `data_hash` + `prev_meta_hash` links.
2. **Content-Addressed Persistence (`CARv1` / `IPFS` / `Cloudflare R2` / `Storacha`)**: `pkaput squash-release` flattens OCI layers, runs Anchore `syft` to generate a CycloneDX v1.5 SBOM, signs an in-toto Statement v1 (`DSSEv1` PAE) + W3C Verifiable Credential (`did:key:z6Mk...` Ed25519) preserving `squashedLineage.precursors`, and packages everything into a deterministic Content-Addressed Archive (`release-bundle.car`).
3. **Registry-Agnostic Discovery (`RFC 4648 Base32 DNSLink`)**: `pkaput publish-dns` maps any squashed OCI layer `sha256:<hex>` digest to `_dnslink.<base32-digest>.<domain>` (`TXT "dnslink=/ipfs/<CID>"`), allowing any consumer to locate the attestation bundle via standard DNS.
4. **Lazy Zero-Trust Pre-Flight Gate (`pkaput preflight-gate`)**: Consumers resolve the DNSLink record, lazily fetch `attestation.dsse.json` (`~2 KB`) before downloading the full SBOM, verify the Ed25519 signature and hash chain, audit exact version upgrade history, and enforce `packablock.policy.yaml`.

### Persistence & Control Plane Comparison

| Storage / Registry Model | Controlled By | Pros | Cons & Failure Modes |
| :--- | :--- | :--- | :--- |
| **1. Platform Transparency Log / OCI Registry** (`Rekor` / `ghcr.io`) | **Microsoft / GitHub / Platform Operator** | Turnkey CI/CD integration (GitHub Actions OIDC, Cosign); zero initial hosting setup. | Vendor lock-in and account suspension risk; squashing OCI layers destroys precursor diff history; tied to a single corporate control plane. |
| **2. 3rd-Party Cloud Object Bucket** (`Amazon S3` / generic blob) | **Amazon (AWS) + Leased Billing Account** | Inexpensive raw blob storage; high uptime while the monthly cloud bill is paid. | Location-addressed URLs (`s3://bucket/key`) can be silently overwritten or deleted; if the publisher stops paying AWS, all downstream verification breaks. |
| **3. 1st-Party Manufacturer Self-Hosted** (`OEM Portal` / private Harbor) | **100% Dependent on Manufacturer** | Full 1st-party control over proprietary release artifacts and hardware manuals. | Single point of failure: if the OEM EOLs the device (e.g., Sonoff S31), goes out of business, or blocks open-source ESPHome reflashing, provenance vanishes. |
| **4. Centralized EU-Wide Registry** (Proposed EU CRA Central Portal) | **EU Regulator & Contracted Cloud Host** | Uniform regulatory schema across EU member states for Cyber Resilience Act (CRA) audits. | Centralized bureaucracy and high-value outage/attack target; still dependent on a contracted hosting provider; fragmented from non-EU open-source ecosystems. |
| **5. Centralized State-Wide Registry** (State / National Gov Portal) | **State Government IT Agency** | Matches local public-sector procurement rules and state compliance mandates. | 50+ incompatible jurisdictional silos; fragile state IT uptime, appropriations cycles, and contractor turnover. |
| **6. Continuous Attestation + `pkaput`** (`CARv1` + `IPFS` + `Base32 DNSLink`) | **Cryptographic Math (`CIDv1` + `W3C did:key`)** | **Content-Addressed & Mirror-Agnostic:** The signed 5-block `chain.packablock`, Anchore `syft` SBOM, `DSSE` statement, and PDF/YAML artifacts hash to a deterministic `bafybei...` CID. Pin simultaneously to R2, Storacha, GitHub Pages, an EU/State mirror, or a local LAN node—any surviving copy verifies identically. | Requires deterministic canonicalization of SBOM timestamps/UUIDs and a lightweight `_dnslink` TXT pointer (both automated by `pkaput squash-release` & `publish-dns`). |

#### Why RFC 4648 Base32 `DNSLink` Instead of an HTTP `/.well-known/` Endpoint?
1. **Decoupled From Origin Server Uptime**: An HTTP `/.well-known/sbom` endpoint relies on the manufacturer's web server, TLS certificates, and CDN staying online forever. DNS is globally replicated and cached across recursive resolvers (`UDP/53`, DoH, DoT, and `DNSSEC`-signed zone files) with zero origin compute required.
2. **Transparent vs. Concealed Access Control**: A `/.well-known/` route conceals routing, rate-limiting, and mutation controls inside the hosting provider's opaque application layer (where a server can silently alter responses or block IPs). Because each `sha256` layer digest maps to a unique 52-char subdomain (`_dnslink.<base32>`), DNS credentials can be strictly scoped to **append-only (`CREATE TXT` only)**—never overwriting prior releases.
3. **Sovereign TLD Governance (`.eu` / `.gov`) Without Blob Lock-In**: Instead of an EU regulator or State government building a monolithic portal to host terabytes of firmware and SBOMs, public agencies can operate an append-only, DNSSEC-signed discovery zone under a sovereign TLD (e.g., `_dnslink.<base32>.cra.europa.eu` or `.wa.gov`) while the `CARv1` bundles remain distributed across IPFS, R2, and local mirrors.
4. **Frictionless Voluntary Adoption (OEMs & Individual Inventory)**: Zero permission is required to start—a hardware manufacturer can adopt this voluntarily today under their existing corporate domain (e.g., `_dnslink.<base32>.devices.sonoff.tech`), and individual builders or homelab operators who reflash their own devices can publish and audit their own fleet inventory under a personal or project subdomain (e.g., `_dnslink.<base32>.examples.packablock.com`).

---

## 1. Featured Live Scenario: ITEAD Sonoff S31 Smart Plug (`ESP8266EX` + `CSE7766`) Firmware Liberation & 3x Home Assistant OTA

Both the [Live Web Verifier](https://examples.packablock.com) and `examples/esphome-s31/` model a complete 5-block hardware-to-firmware supply-chain lifecycle for an **ITEAD Sonoff S31 Smart Plug** (`FCC ID: 2APN5S31`, `ESP8266EX` + `CSE7766` energy monitoring IC):

| Block | Event | Firmware / Dependency Transition | Attached Artifact / Provenance Link |
| :--- | :--- | :--- | :--- |
| **Block `#0`** | `factory_oem_baseline` | `itead/ewelink-oem-firmware@2.6.1`, `espressif/esp8266-nonos-sdk@2.2.1` | [`owners-manual.pdf`](examples/esphome-s31/owners-manual.pdf) (`bafkreih2dwc3wrfoli4fvnldafjaoj2zkdphnmdapzff3l52lp6nkvwpqu`) |
| **Block `#1`** | `uart_liberation_flash` | Flashed to open-source **ESPHome `2026.7.0`** (3 versions back), removing the proprietary OEM SDK | [`sonoff-s31-esphome.yaml`](examples/esphome-s31/sonoff-s31-esphome.yaml) (`bafkreibbzyj5gy3yrcuxyzhrs3yiv6352j4oali53dqstiij5x6e3c3vga`) + [`devices.esphome.io/devices/Sonoff-S31`](https://devices.esphome.io/devices/Sonoff-S31) |
| **Block `#2`** | `ha_esphome_ota_update` (`#1`) | Home Assistant ESPHome OTA #1: `esphome/esphome` `2026.7.0` → `2026.8.2`, `lwip/lwip-esp8266` `2.1.2` → `2.1.3-patch1` (Wi-Fi beacon stability) | Signed `ed25519` block `730b6ba2ca5e...` |
| **Block `#3`** | `ha_esphome_ota_update` (`#2`) | Home Assistant ESPHome OTA #2: `esphome/esphome` `2026.8.2` → `2026.9.1`, `esphome/ESPAsyncWebServer` `3.1.0` → `3.3.2` (**remediates `CVE-2026-4108`**) | Signed `ed25519` block `dfef64f18ba5...` |
| **Block `#4`** | `ha_esphome_ota_update` (`#3`) | Home Assistant ESPHome OTA #3 (Present Day): `esphome/esphome` `2026.9.1` → `2026.10.3`, `esphome/noise-c` `0.1.9` → `0.1.10` (Noise API key rotation) | Terminal `meta_hash`: `72da938cfb25373a289f4f9c67d868d6224d4602debd509ada66da087c34daa2` |

### Run the Sonoff S31 ESPHome Scenario via CLI
```powershell
# 1. Generate & verify the 5-block Sonoff S31 chain
./bin/pkaput.exe init-esphome-demo --chain examples/esphome-s31/packablock.yaml
./bin/pkaput.exe check examples/esphome-s31/packablock.yaml

# 2. Squash the 4 ESPHome firmware layers, run Anchore Syft, & attach Owner's Manual PDF + ESPHome YAML into CARv1
./bin/pkaput.exe squash-release `
  --chain examples/esphome-s31/packablock.yaml `
  --layers 4 `
  --output-tag "sonoff-s31-esphome:2026.10.3" `
  --attach "examples/esphome-s31/owners-manual.pdf,examples/esphome-s31/sonoff-s31-esphome.yaml" `
  --storage local

# 3. Publish the RFC 4648 Base32 DNSLink record
./bin/pkaput.exe publish-dns --domain "examples.packablock.com" --provider coredns

# 4. Run the Consumer 3-Gate Zero-Trust Pre-Flight Verification
./bin/pkaput.exe preflight-gate `
  --file bin/layers/flattened.tar.gz `
  --domain "examples.packablock.com" `
  --policy examples/esphome-s31/packablock.policy.yaml `
  --query-pkg "esphome/esphome" `
  --query-version "2026.10.3"
```

---

## 2. Quickstart & End-to-End Local Demo (`secure-lib:v1.0.0`)

### Build `pkaput` & Run Unit Tests
```powershell
go build -o bin/pkaput.exe ./cmd/pkaput
go test -v ./...
```

### Execute the 4-Stage Producer-to-Consumer Workflow (`pkablk` → `syft` → `pkaput`)
```powershell
# 1. Producer: Verify packablock.yaml chain, run Anchore Syft to generate sbom.cdx.json, and squash layers into CARv1
#    (Uses 'syft' on PATH if installed in CI/CD, or auto-bootstraps official Anchore Syft v1.20.0 into ./bin/ on first run)
./bin/pkaput.exe squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --storage local

#    Or bring your own Syft CycloneDX output from an upstream CI step:
#    syft scan dir:. -o cyclonedx-json=bin/raw-syft.cdx.json
#    ./bin/pkaput.exe squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --sbom-in bin/raw-syft.cdx.json --storage local

# 2. Producer: Publish RFC 4648 Base32 DNSLink TXT pointer to CoreDNS (or --provider cloudflare)
./bin/pkaput.exe publish-dns --domain "examples.packablock.com" --provider coredns

# 3. Consumer: Run Zero-Trust Pre-Flight Gate against the release layer tarball (or against a live HTTP gateway)
./bin/pkaput.exe preflight-gate --file bin/layers/flattened.tar.gz --domain "examples.packablock.com"
```

### Optional: Start Local Containerized Stack (`registry:2`, `ipfs/kubo`, `coredns`)
```powershell
docker compose up -d
```

---

## 3. Persistent Edge Storage Mode (`Cloudflare Worker + R2 + Cloudflare DNS`)

```powershell
# Deploy the Interactive Verifier & Trustless IPFS Gateway Worker
cd worker
npx wrangler deploy

# Put release bundle to R2 + Storacha and publish DNSLink to Cloudflare DNS
$env:PKAPUT_WORKER_URL = "https://examples.packablock.com"
$env:PKAPUT_WORKER_TOKEN = "<your-worker-put-token>"
$env:CF_API_TOKEN = "<your-cloudflare-dns-token>"

./bin/pkaput.exe squash-release --layers 3 --output-tag "secure-lib:v1.0.0" --storage r2
./bin/pkaput.exe publish-dns --domain "examples.packablock.com" --provider cloudflare
./bin/pkaput.exe preflight-gate --file bin/layers/flattened.tar.gz --domain "examples.packablock.com" --ipfs-gateway "https://examples.packablock.com,https://aaronbronow.github.io/continuous-attestation,https://w3s.link"
```
