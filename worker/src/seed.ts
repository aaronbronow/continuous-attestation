export interface SeedRelease {
	outputTag: string;
	flattenedDigest: string;
	base32Label: string;
	dnslinkFqdn: string;
	dnslinkTxt: string;
	rootCid: string;
	issuerDid: string;
	publicKeyHex: string;
	terminalMetaHash: string;
	files: Record<string, string>;
}

export const SEED_CID = "bafybeide5ligei4pht5cl2t4zsqf2bul2edlwhvpmc4uacsfupxod7uute";
export const SEED_DIGEST = "sha256:aef8959f6e3ecf9201eb237c02779f2b3f3a4676db62a3575181c248e5a65f2a";
export const SEED_LABEL = "v34jlh3oh3hzeaplen6ae547fm7turtw3nrkgv2rqhberzngl4va";

export const SEED_RELEASE: SeedRelease = {
	outputTag: "secure-lib:v1.0.0",
	flattenedDigest: SEED_DIGEST,
	base32Label: SEED_LABEL,
	dnslinkFqdn: `_dnslink.${SEED_LABEL}.layers.packablock.com`,
	dnslinkTxt: `dnslink=/ipfs/${SEED_CID}`,
	rootCid: SEED_CID,
	issuerDid: "did:key:z6MktwupdmLXVVqTzCw4i46r4uGyosGXRnR3XjN4Zq7oMMsw",
	publicKeyHex: "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
	terminalMetaHash: "8fec19173e2dd9b321bd9f45c3ac20c48875f9b69abcb640fd5136e18d9e322e",
	files: {
		"subject.json": `{
  "name": "secure-lib:v1.0.0",
  "digest": {
    "sha256": "aef8959f6e3ecf9201eb237c02779f2b3f3a4676db62a3575181c248e5a65f2a"
  },
  "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
  "size": 574
}
`,
		"chain.packablock": `lockfiles:
  package-lock.json:
    packages:
      - github.com/gin-gonic/gin: 1.9.1
      - golang.org/x/crypto: 0.28.0
      - gopkg.in/yaml.v3: 3.0.1
---
$yaml-chain-meta:
  version: 1.0.0
  block_index: 0
  timestamp: 2026-11-04T07:27:19.413Z
  hashing_strategy: raw
  data_hash: 23f4b53598bed5c022d74a8bd94fd2f111b91deb2a290664a0a93d6872ceb426
  prev_meta_hash: "0000000000000000000000000000000000000000000000000000000000000000"
  meta_hash: 7bc34b2058c4c68d61497e3f240257eaf689ef81eab8c44426613478899944b1
---
lockfiles:
  package-lock.json:
    packages:
      - github.com/gin-gonic/gin:
          - old: 1.9.1
          - new: 1.10.0
          - loc: 12,9
---
$yaml-chain-meta:
  version: 1.0.0
  block_index: 1
  timestamp: 2026-11-04T07:27:19.765Z
  hashing_strategy: raw
  data_hash: 2b13b18bf6385ff9817a20d52c75ef2761c97ea424b48f2d2c1b102f4b428ca0
  prev_meta_hash: 7bc34b2058c4c68d61497e3f240257eaf689ef81eab8c44426613478899944b1
  meta_hash: 8fec19173e2dd9b321bd9f45c3ac20c48875f9b69abcb640fd5136e18d9e322e
`,
		"sbom.cdx.json": `{
  "bomFormat": "CycloneDX",
  "components": [
    {
      "type": "library",
      "name": "github.com/gin-gonic/gin",
      "version": "1.10.0",
      "purl": "pkg:golang/github.com/gin-gonic/gin@v1.10.0",
      "licenses": [
        {
          "license": {
            "id": "MIT"
          }
        }
      ]
    },
    {
      "type": "library",
      "name": "golang.org/x/crypto",
      "version": "0.28.0",
      "purl": "pkg:golang/golang.org/x/crypto@v0.28.0",
      "licenses": [
        {
          "license": {
            "id": "MIT"
          }
        }
      ]
    },
    {
      "type": "library",
      "name": "gopkg.in/yaml.v3",
      "version": "3.0.1",
      "purl": "pkg:golang/gopkg.in/yaml.v3@v3.0.1",
      "licenses": [
        {
          "license": {
            "id": "MIT"
          }
        }
      ]
    }
  ],
  "metadata": {
    "component": {
      "name": "secure-lib:v1.0.0",
      "type": "container",
      "version": "sha256:aef8959f6e3ecf9201eb237c02779f2b3f3a4676db62a3575181c248e5a65f2a"
    },
    "timestamp": "2026-11-04T16:58:34Z"
  },
  "serialNumber": "urn:uuid:7b89c4f1-903a-41b2-a8d0-aef8959f6e3e",
  "specVersion": "1.5",
  "version": 1,
  "vulnerabilities": []
}
`,
		"attestation.dsse.json": `{
  "payloadType": "application/vnd.in-toto+json",
  "payload": "ewogICJfdHlwZSI6ICJodHRwczovL2luLXRvdG8uaW8vU3RhdGVtZW50L3YxIiwKICAic3ViamVjdCI6IFsKICAgIHsKICAgICAgIm5hbWUiOiAic2VjdXJlLWxpYjp2MS4wLjAiLAogICAgICAiZGlnZXN0IjogewogICAgICAgICJzaGEyNTYiOiAiYWVmODk1OWY2ZTNlY2Y5MjAxZWIyMzdjMDI3NzlmMmIzZjNhNDY3NmRiNjJhMzU3NTE4MWMyNDhlNWE2NWYyYSIKICAgICAgfSwKICAgICAgIm1lZGlhVHlwZSI6ICJhcHBsaWNhdGlvbi92bmQub2NpLmltYWdlLmxheWVyLnYxLnRhcitnemlwIiwKICAgICAgInNpemUiOiA1NzQKICAgIH0KICBdLAogICJwcmVkaWNhdGVUeXBlIjogImh0dHBzOi8vZG9jcy5wYWNrYWJsb2NrLmNvbS9zcGVjL3NxdWFzaC1wcmVkaWNhdGUvdjEiLAogICJwcmVkaWNhdGUiOiB7CiAgICAib3V0cHV0VGFnIjogInNlY3VyZS1saWI6djEuMC4wIiwKICAgICJmbGF0dGVuZWRMYXllckRpZ2VzdCI6ICJzaGEyNTY6YWVmODk1OWY2ZTNlY2Y5MjAxZWIyMzdjMDI3NzlmMmIzZjNhNDY3NmRiNjJhMzU3NTE4MWMyNDhlNWE2NWYyYSIsCiAgICAiaXNzdWVyRGlkIjogImRpZDprZXk6ejZNa3R3dXBkbUxYVlZxVHpDdzRpNDZyNHVHeW9zR1hSblIzWGpONFpxN29NTXN3IiwKICAgICJ0aW1lc3RhbXAiOiAiMjAyNi0xMS0wNFQxNjo1ODozNFoiLAogICAgInNxdWFzaGVkTGluZWFnZSI6IHsKICAgICAgImxheWVyQ291bnQiOiAyLAogICAgICAidGVybWluYWxNZXRhSGFzaCI6ICI4ZmVjMTkxNzNlMmRkOWIzMjFiZDlmNDVjM2FjMjBjNDg4NzVmOWI2OWFiY2I2NDBmZDUxMzZlMThkOWUzMjJlIiwKICAgICAgInByZWN1cnNvcnMiOiBbCiAgICAgICAgewogICAgICAgICAgImJsb2NrSW5kZXgiOiAwLAogICAgICAgICAgImJsb2NrQ2lkIjogImJhZmtyZWlmbG4zZWNlbWdkZ3VhdDJpZGJpenk3YWVxYWR1b2loNmdzZmJua2Zhb3NpanR5ZmRodXk0IiwKICAgICAgICAgICJwYXJlbnRCbG9ja0NpZCI6ICJiYWZrcmVpYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYSIsCiAgICAgICAgICAibWV0YUhhc2giOiAiN2JjMzRiMjA1OGM0YzY4ZDYxNDk3ZTNmMjQwMjU3ZWFmNjg5ZWY4MWVhYjhjNDQ0MjY2MTM0Nzg4OTk5NDRiMSIsCiAgICAgICAgICAiZGF0YUhhc2giOiAiMjNmNGI1MzU5OGJlZDVjMDIyZDc0YThiZDk0ZmQyZjExMWI5MWRlYjJhMjkwNjY0YTBhOTNkNjg3MmNlYjQyNiIsCiAgICAgICAgICAibGF5ZXJEaWdlc3QiOiAiIiwKICAgICAgICAgICJ0aW1lc3RhbXAiOiAiMjAyNi0xMS0wNFQwNzoyNzoxOS40MTNaIiwKICAgICAgICAgICJhY3RvciI6ICJyZWxlYXNlLWJvdEBwYWNrYWJsb2NrLmNvbSIsCiAgICAgICAgICAiY29tbWl0IjogIkhFQUQiCiAgICAgICAgfSwKICAgICAgICB7CiAgICAgICAgICAiYmxvY2tJbmRleCI6IDEsCiAgICAgICAgICAiYmxvY2tDaWQiOiAiYmFma3JlaWd3aGlqdmFpZWY3ZG1wZnVxY2w0czRpZm92NjY2dmc0bmZ5MjYyN2tiZWF3cTVjZ25lY20iLAogICAgICAgICAgInBhcmVudEJsb2NrQ2lkIjogImJhZmtyZWlmbG4zZWNlbWdkZ3VhdDJpZGJpenk3YWVxYWR1b2loNmdzZmJua2Zhb3NpanR5ZmRodXk0IiwKICAgICAgICAgICJtZXRhSGFzaCI6ICI4ZmVjMTkxNzNlMmRkOWIzMjFiZDlmNDVjM2FjMjBjNDg4NzVmOWI2OWFiY2I2NDBmZDUxMzZlMThkOWUzMjJlIiwKICAgICAgICAgICJkYXRhSGFzaCI6ICIyYjEzYjE4YmY2Mzg1ZmY5ODE3YTIwZDUyYzc1ZWYyNzYxYzk3ZWE0MjRiNDhmMmQyYzFiMTAyZjRiNDI4Y2EwIiwKICAgICAgICAgICJsYXllckRpZ2VzdCI6ICIiLAogICAgICAgICAgInRpbWVzdGFtcCI6ICIyMDI2LTExLTA0VDA3OjI3OjE5Ljc2NVoiLAogICAgICAgICAgImFjdG9yIjogInJlbGVhc2UtYm90QHBhY2thYmxvY2suY29tIiwKICAgICAgICAgICJjb21taXQiOiAiSEVBRCIKICAgICAgICB9CiAgICAgIF0KICAgIH0sCiAgICAidmVyaWZpY2F0aW9uU3VtbWFyeSI6IHsKICAgICAgInZlcmlmaWVyIjogewogICAgICAgICJpZCI6ICJkaWQ6a2V5Ono2TWt0d3VwZG1MWFZWcVR6Q3c0aTQ2cjR1R3lvc0dYUm5SM1hqTjRacTdvTU1zdyIsCiAgICAgICAgInVyaSI6ICJodHRwczovL2RvY3MucGFja2FibG9jay5jb20vc3BlYy8jNDQtdmVyaWZpY2F0aW9uLXN1bW1hcnktYXR0ZXN0YXRpb25zLXZzYSIKICAgICAgfSwKICAgICAgInRpbWVWZXJpZmllZCI6ICIyMDI2LTExLTA0VDE2OjU4OjM0WiIsCiAgICAgICJyZXNvdXJjZVVyaSI6ICJvY2k6Ly9zZWN1cmUtbGliOnYxLjAuMEBzaGEyNTY6YWVmODk1OWY2ZTNlY2Y5MjAxZWIyMzdjMDI3NzlmMmIzZjNhNDY3NmRiNjJhMzU3NTE4MWMyNDhlNWE2NWYyYSIsCiAgICAgICJwb2xpY3kiOiB7CiAgICAgICAgImRpZ2VzdCI6ICJiMzIwN2EyMWJlYjA0YWIwMTFlZWYyNDA1NzAyOTg1NmU4ZDQ2MDYwODUwNjRiNzE4NTc4YTk3MTNmMTBhOTdhIiwKICAgICAgICAidXJpIjogImh0dHBzOi8vZG9jcy5wYWNrYWJsb2NrLmNvbS9zcGVjLyM0Ny1wb2xpY3ktY29udHJvbC1zY2hlbWEtYW5kLWVuZm9yY2VtZW50LXNwZWNpZmljYXRpb24iCiAgICAgIH0sCiAgICAgICJpbnB1dEF0dGVzdGF0aW9ucyI6IFsKICAgICAgICB7CiAgICAgICAgICAibWVkaWFUeXBlIjogImFwcGxpY2F0aW9uL3ZuZC5wYWNrYWJsb2NrLm1hbmlmZXN0LWNoYWluK3lhbWwiLAogICAgICAgICAgInNoYTI1NiI6ICI4ZmVjMTkxNzNlMmRkOWIzMjFiZDlmNDVjM2FjMjBjNDg4NzVmOWI2OWFiY2I2NDBmZDUxMzZlMThkOWUzMjJlIiwKICAgICAgICAgICJ1cmkiOiAiaXBmczovL2NoYWluLnBhY2thYmxvY2siCiAgICAgICAgfQogICAgICBdLAogICAgICAidmVyaWZpY2F0aW9uUmVzdWx0IjogIlBBU1NFRCIsCiAgICAgICJ2ZXJpZmllZExldmVscyI6IFsKICAgICAgICAiU0xTQV9CVUlMRF9MRVZFTF8zIiwKICAgICAgICAiUEFDS0FCTE9DS19DQV9MRVZFTF8yIgogICAgICBdCiAgICB9LAogICAgInZlcmlmaWFibGVDcmVkZW50aWFsIjogewogICAgICAiQGNvbnRleHQiOiBbCiAgICAgICAgImh0dHBzOi8vd3d3LnczLm9yZy8yMDE4L2NyZWRlbnRpYWxzL3YxIiwKICAgICAgICAiaHR0cHM6Ly9kb2NzLnBhY2thYmxvY2suY29tL2NvbnRleHRzL2NvbnRpbnVvdXMtYXR0ZXN0YXRpb24vdjEiCiAgICAgIF0sCiAgICAgICJ0eXBlIjogWwogICAgICAgICJWZXJpZmlhYmxlQ3JlZGVudGlhbCIsCiAgICAgICAgIlBhY2thYmxvY2tTcXVhc2hBdHRlc3RhdGlvbkNyZWRlbnRpYWwiCiAgICAgIF0sCiAgICAgICJpc3N1ZXIiOiAiZGlkOmtleTp6Nk1rdHd1cGRtTFhWVnFUekN3NGk0NnI0dUd5b3NHWFJuUjNYak40WnE3b01Nc3ciLAogICAgICAiaXNzdWFuY2VEYXRlIjogIjIwMjYtMTEtMDRUMTY6NTg6MzRaIiwKICAgICAgImNyZWRlbnRpYWxTdWJqZWN0IjogewogICAgICAgICJibG9ja0NvdW50IjogMiwKICAgICAgICAiZmxhdHRlbmVkTGF5ZXJEaWdlc3QiOiAic2hhMjU2OmFlZjg5NTlmNmUzZWNmOTIwMWViMjM3YzAyNzc5ZjJiM2YzYTQ2NzZkYjYyYTM1NzUxODFjMjQ4ZTVhNjVmMmEiLAogICAgICAgICJpZCI6ICJ1cm46aWV0ZjpwYXJhbXM6b2F1dGg6Y2xpZW50LWFzc2VydGlvbi10eXBlOm9jaS1sYXllcjpzaGEyNTY6YWVmODk1OWY2ZTNlY2Y5MjAxZWIyMzdjMDI3NzlmMmIzZjNhNDY3NmRiNjJhMzU3NTE4MWMyNDhlNWE2NWYyYSIsCiAgICAgICAgIm91dHB1dFRhZyI6ICJzZWN1cmUtbGliOnYxLjAuMCIsCiAgICAgICAgInRlcm1pbmFsTWV0YUhhc2giOiAiOGZlYzE5MTczZTJkZDliMzIxYmQ5ZjQ1YzNhYzIwYzQ4ODc1ZjliNjlhYmNiNjQwZmQ1MTM2ZTE4ZDllMzIyZSIKICAgICAgfSwKICAgICAgInByb29mIjogewogICAgICAgICJjcmVhdGVkIjogIjIwMjYtMTEtMDRUMTY6NTg6MzRaIiwKICAgICAgICAicHJvb2ZQdXJwb3NlIjogImFzc2VydGlvbk1ldGhvZCIsCiAgICAgICAgInByb29mVmFsdWUiOiAidDZ6SHRXdlFJWk12U2Q2blErSWJyUGNadHZjTEtuMXVGZ1VOTUwrUCsxN3Z5OWdqOEhYLzdvYUlvTXRmd3dtZ3JFV3VTekxOT1czbUZRY0hCM3o3Q2c9PSIsCiAgICAgICAgInR5cGUiOiAiRWQyNTUxOVNpZ25hdHVyZTIwMjAiLAogICAgICAgICJ2ZXJpZmljYXRpb25NZXRob2QiOiAiZGlkOmtleTp6Nk1rdHd1cGRtTFhWVnFUekN3NGk0NnI0dUd5b3NHWFJuUjNYak40WnE3b01Nc3cjejZNa3R3dXBkbUxYVlZxVHpDdzRpNDZyNHVHeW9zR1hSblIzWGpONFpxN29NTXN3IgogICAgICB9CiAgICB9CiAgfQp9",
  "signatures": [
    {
      "keyid": "did:key:z6MktwupdmLXVVqTzCw4i46r4uGyosGXRnR3XjN4Zq7oMMsw",
      "sig": "okd0+S6dYASQH1z/b7Ux/p1LxjWcTyHMBFQpp2cb1/gGE1ol6Rx+WnybkIcaQKFAsMcWQSw+Cv3GnMv6wNmPCA=="
    }
  ]
}
`
	}
};
