import fs from "node:fs";
import path from "node:path";

const SEED_CID = "bafybeide5ligei4pht5cl2t4zsqf2bul2edlwhvpmc4uacsfupxod7uute";
const SEED_DIGEST = "sha256:aef8959f6e3ecf9201eb237c02779f2b3f3a4676db62a3575181c248e5a65f2a";
const SEED_LABEL = "v34jlh3oh3hzeaplen6ae547fm7turtw3nrkgv2rqhberzngl4va";

const distDir = path.resolve("dist");
const ipfsDir = path.join(distDir, "ipfs", SEED_CID);
fs.mkdirSync(ipfsDir, { recursive: true });

// Read worker/src/seed.ts and worker/src/ui.ts to generate the static HTML + IPFS files
const seedTs = fs.readFileSync(path.resolve("worker/src/seed.ts"), "utf8");
const uiTs = fs.readFileSync(path.resolve("worker/src/ui.ts"), "utf8");

// Extract files from bin/ipfs-cache if present, otherwise use seed.ts
const cacheDir = path.resolve("bin/ipfs-cache", SEED_CID);
const files = {};
const fileNames = ["subject.json", "chain.packablock", "sbom.cdx.json", "attestation.dsse.json"];

for (const name of fileNames) {
	const cachePath = path.join(cacheDir, name);
	if (fs.existsSync(cachePath)) {
		files[name] = fs.readFileSync(cachePath, "utf8");
	}
}

// Fallback extraction if running without prior build
if (Object.keys(files).length < 4) {
	files["chain.packablock"] = fs.readFileSync(path.resolve("packablock.yaml"), "utf8");
}

for (const [name, content] of Object.entries(files)) {
	fs.writeFileSync(path.join(ipfsDir, name), content, "utf8");
}

// Write DNSLink JSON endpoint
const apiDir = path.join(distDir, "api");
fs.mkdirSync(apiDir, { recursive: true });
fs.writeFileSync(
	path.join(apiDir, "dnslink.json"),
	JSON.stringify(
		{
			digest: SEED_DIGEST,
			base32Label: SEED_LABEL,
			fqdn: `_dnslink.${SEED_LABEL}.layers.packablock.com`,
			txt: `dnslink=/ipfs/${SEED_CID}`,
			cid: SEED_CID,
		},
		null,
		2
	)
);

// Evaluate renderLandingPage for static hosting
const origin = process.env.PUBLIC_ORIGIN || "https://aaronbronow.github.io/continuous-attestation";
const templateMatch = uiTs.match(/return `([\s\S]*)`;\s*\}/);
if (templateMatch) {
	const SEED_RELEASE = {
		outputTag: "secure-lib:v1.0.0",
		flattenedDigest: SEED_DIGEST,
		base32Label: SEED_LABEL,
		dnslinkFqdn: `_dnslink.${SEED_LABEL}.layers.packablock.com`,
		dnslinkTxt: `dnslink=/ipfs/${SEED_CID}`,
		rootCid: SEED_CID,
		issuerDid: "did:key:z6MktwupdmLXVVqTzCw4i46r4uGyosGXRnR3XjN4Zq7oMMsw",
		publicKeyHex: "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
		terminalMetaHash: "8fec19173e2dd9b321bd9f45c3ac20c48875f9b69abcb640fd5136e18d9e322e",
		files,
	};

	const renderFn = new Function(
		"origin",
		"SEED_CID",
		"SEED_DIGEST",
		"SEED_LABEL",
		"SEED_RELEASE",
		`return \`${templateMatch[1]}\`;`
	);
	const html = renderFn(origin, SEED_CID, SEED_DIGEST, SEED_LABEL, SEED_RELEASE);
	fs.writeFileSync(path.join(distDir, "index.html"), html, "utf8");
	fs.writeFileSync(path.join(distDir, ".nojekyll"), "", "utf8");
	console.log(`Built static verifier + IPFS gateway tree in ${distDir}`);
}
