import fs from "node:fs";
import path from "node:path";

const distDir = path.resolve("dist");
fs.mkdirSync(distDir, { recursive: true });

// Read worker/src/seed.ts and strip TypeScript interface/type annotations to evaluate directly
const seedTs = fs.readFileSync(path.resolve("worker/src/seed.ts"), "utf8");
const seedJs = seedTs
	.replace(/export interface SeedRelease \{[\s\S]*?\}\n/m, "")
	.replace(/export const (\w+)(?::\s*[^=]+)?\s*=/g, "const $1 =")
	.concat("\nreturn { ESPHOME_CID, ESPHOME_DIGEST, ESPHOME_LABEL, SEED_CID, SEED_DIGEST, SEED_LABEL, ESPHOME_RELEASE, SEED_RELEASE, ALL_RELEASES };");

const {
	ESPHOME_CID,
	ESPHOME_DIGEST,
	ESPHOME_LABEL,
	SEED_CID,
	SEED_DIGEST,
	SEED_LABEL,
	ESPHOME_RELEASE,
	SEED_RELEASE,
	ALL_RELEASES,
} = new Function(seedJs)();

// Write content-addressed IPFS directory trees for all releases
for (const release of Object.values(ALL_RELEASES)) {
	const ipfsDir = path.join(distDir, "ipfs", release.rootCid);
	fs.mkdirSync(ipfsDir, { recursive: true });
	for (const [name, content] of Object.entries(release.files)) {
		fs.writeFileSync(path.join(ipfsDir, name), content, "utf8");
	}
}

// Write DNSLink JSON endpoint with all releases and default pointer to ESPHome S31
const apiDir = path.join(distDir, "api");
fs.mkdirSync(apiDir, { recursive: true });
fs.writeFileSync(
	path.join(apiDir, "dnslink.json"),
	JSON.stringify(
		{
			digest: ESPHOME_DIGEST,
			base32Label: ESPHOME_LABEL,
			fqdn: ESPHOME_RELEASE.dnslinkFqdn,
			txt: ESPHOME_RELEASE.dnslinkTxt,
			cid: ESPHOME_CID,
			releases: Object.values(ALL_RELEASES).map((r) => ({
				id: r.id,
				outputTag: r.outputTag,
				digest: r.flattenedDigest,
				base32Label: r.base32Label,
				fqdn: r.dnslinkFqdn,
				txt: r.dnslinkTxt,
				cid: r.rootCid,
			})),
		},
		null,
		2
	)
);

// Evaluate renderLandingPage for static hosting
const uiTs = fs.readFileSync(path.resolve("worker/src/ui.ts"), "utf8");
const origin = process.env.PUBLIC_ORIGIN || "https://aaronbronow.github.io/continuous-attestation";
const templateMatch = uiTs.match(/return `([\s\S]*)`;\s*\}/);
if (!templateMatch) {
	throw new Error("Could not extract HTML template from worker/src/ui.ts");
}

const renderFn = new Function(
	"origin",
	"ESPHOME_RELEASE",
	"SEED_RELEASE",
	`return \`${templateMatch[1]}\`;`
);
const html = renderFn(origin, ESPHOME_RELEASE, SEED_RELEASE);
fs.writeFileSync(path.join(distDir, "index.html"), html, "utf8");
fs.writeFileSync(path.join(distDir, ".nojekyll"), "", "utf8");
console.log(`Built static verifier + IPFS gateway trees in ${distDir} (${Object.keys(ALL_RELEASES).length} releases)`);
