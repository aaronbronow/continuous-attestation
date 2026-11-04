import {
	ALL_RELEASES,
	ESPHOME_CID,
	ESPHOME_DIGEST,
	ESPHOME_LABEL,
	ESPHOME_RELEASE,
	SEED_CID,
	SEED_DIGEST,
	SEED_LABEL,
	SEED_RELEASE,
} from "./seed";
import { renderLandingPage } from "./ui";

export interface Env {
	ATTESTATION_BUCKET?: R2Bucket;
	PKAPUT_WORKER_TOKEN?: string;
	PUBLIC_GATEWAY_ORIGIN?: string;
}

const IMMUTABLE_CACHE_CONTROL = "public, max-age=31536000, immutable";

// Ephemeral in-memory fallback map when R2 binding is omitted in preview/unauthenticated deployments
const memoryStore = new Map<string, { body: Uint8Array; contentType: string }>();

function detectContentType(filename: string): string {
	if (filename === "chain.packablock") {
		return "application/vnd.packablock.manifest-chain+yaml; charset=utf-8";
	}
	if (filename.endsWith(".yaml") || filename.endsWith(".yml")) {
		return "text/yaml; charset=utf-8";
	}
	if (filename === "attestation.dsse.json") {
		return "application/vnd.in-toto+json";
	}
	if (filename === "sbom.cdx.json") {
		return "application/vnd.cyclonedx+json";
	}
	if (filename.endsWith(".json")) {
		return "application/json; charset=utf-8";
	}
	if (filename.endsWith(".car")) {
		return "application/vnd.ipld.car";
	}
	if (filename.endsWith(".pdf")) {
		return "application/pdf";
	}
	return "application/octet-stream";
}

function timingSafeEqualString(a: string, b: string): boolean {
	const enc = new TextEncoder();
	const bufA = enc.encode(a);
	const bufB = enc.encode(b);
	if (bufA.byteLength !== bufB.byteLength) {
		return false;
	}
	let diff = 0;
	for (let i = 0; i < bufA.byteLength; i++) {
		diff |= bufA[i] ^ bufB[i];
	}
	return diff === 0;
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);
		const path = url.pathname;
		const origin = env.PUBLIC_GATEWAY_ORIGIN || url.origin;

		// 0. Interactive Live Demo UI & API Discovery Endpoints
		if ((request.method === "GET" || request.method === "HEAD") && (path === "/" || path === "/index.html")) {
			return new Response(renderLandingPage(origin), {
				status: 200,
				headers: {
					"Content-Type": "text/html; charset=utf-8",
					"Cache-Control": "public, max-age=60",
					"Access-Control-Allow-Origin": "*",
				},
			});
		}

		if (request.method === "GET" && path === "/api/dnslink") {
			const qDigest = url.searchParams.get("digest") || "";
			const qLabel = url.searchParams.get("label") || "";
			const isGoScenario = qDigest === SEED_DIGEST || qLabel === SEED_LABEL;

			const digest = isGoScenario ? SEED_DIGEST : qDigest || ESPHOME_DIGEST;
			const label = isGoScenario ? SEED_LABEL : qLabel || ESPHOME_LABEL;
			const cid = isGoScenario ? SEED_CID : ESPHOME_CID;

			return new Response(
				JSON.stringify(
					{
						digest,
						base32Label: label,
						fqdn: `_dnslink.${label}.layers.packablock.com`,
						txt: `dnslink=/ipfs/${cid}`,
						cid,
						gatewayUrl: `${origin}/ipfs/${cid}/`,
					},
					null,
					2
				),
				{
					status: 200,
					headers: {
						"Content-Type": "application/json; charset=utf-8",
						"Access-Control-Allow-Origin": "*",
					},
				}
			);
		}

		if (request.method === "GET" && path === "/api/releases") {
			return new Response(JSON.stringify({ releases: [ESPHOME_RELEASE, SEED_RELEASE] }, null, 2), {
				status: 200,
				headers: {
					"Content-Type": "application/json; charset=utf-8",
					"Access-Control-Allow-Origin": "*",
				},
			});
		}

		// 1. Authenticated PUT endpoints for `pkaput squash-release --storage r2`
		if (request.method === "PUT") {
			const auth = request.headers.get("Authorization") || "";
			const expectedAuth = env.PKAPUT_WORKER_TOKEN ? `Bearer ${env.PKAPUT_WORKER_TOKEN}` : "";
			if (!expectedAuth || !timingSafeEqualString(auth, expectedAuth)) {
				return new Response(JSON.stringify({ error: "Unauthorized" }), {
					status: 401,
					headers: { "Content-Type": "application/json" },
				});
			}

			// PUT /ipfs/<cid>/<filename> or PUT /car/<cid>.car
			const key = path.replace(/^\/+/, "");
			if (!key.startsWith("ipfs/") && !key.startsWith("car/")) {
				return new Response(JSON.stringify({ error: "Invalid R2 key prefix" }), {
					status: 400,
					headers: { "Content-Type": "application/json" },
				});
			}

			const contentType = request.headers.get("Content-Type") || detectContentType(key);
			if (env.ATTESTATION_BUCKET) {
				await env.ATTESTATION_BUCKET.put(key, request.body, {
					httpMetadata: {
						contentType,
						cacheControl: IMMUTABLE_CACHE_CONTROL,
					},
				});
			} else {
				const buf = new Uint8Array(await request.arrayBuffer());
				memoryStore.set(key, { body: buf, contentType });
			}

			return new Response(JSON.stringify({ stored: true, key }), {
				status: 201,
				headers: { "Content-Type": "application/json" },
			});
		}

		// 2. Public Zero-Trust GET endpoints (`/ipfs/<cid>/<filename>` and Trustless CAR Gateway `/ipfs/<cid>?format=car`)
		if (request.method !== "GET" && request.method !== "HEAD") {
			return new Response("Method Not Allowed", { status: 405 });
		}

		const cache = caches.default;
		const cachedResponse = await cache.match(request);
		if (cachedResponse) {
			return cachedResponse;
		}

		// Trustless CARv1 Gateway request: GET /ipfs/<cid>?format=car or Accept: application/vnd.ipld.car
		const ipfsMatch = path.match(/^\/ipfs\/([a-z0-9]+)(?:\/(.*))?$/i);
		if (!ipfsMatch) {
			return new Response("Not Found", { status: 404 });
		}

		const cid = ipfsMatch[1];
		const subpath = ipfsMatch[2] || "";
		const wantsCAR =
			url.searchParams.get("format") === "car" ||
			(request.headers.get("Accept") || "").includes("application/vnd.ipld.car");

		const r2Key = wantsCAR && !subpath ? `car/${cid}.car` : `ipfs/${cid}/${subpath || "subject.json"}`;

		if (env.ATTESTATION_BUCKET) {
			const obj = await env.ATTESTATION_BUCKET.get(r2Key);
			if (obj) {
				const headers = new Headers();
				obj.writeHttpMetadata(headers);
				headers.set("ETag", obj.httpEtag);
				headers.set("Cache-Control", IMMUTABLE_CACHE_CONTROL);
				headers.set("X-Ipfs-Path", path);
				headers.set("Access-Control-Allow-Origin", "*");
				if (!headers.has("Content-Type")) {
					headers.set("Content-Type", detectContentType(r2Key));
				}

				const response = new Response(obj.body, { status: 200, headers });
				ctx.waitUntil(cache.put(request, response.clone()));
				return response;
			}
		}

		// Check in-memory store or built-in seed release bundles
		const memObj = memoryStore.get(r2Key);
		if (memObj) {
			return new Response(memObj.body, {
				status: 200,
				headers: {
					"Content-Type": memObj.contentType,
					"Cache-Control": IMMUTABLE_CACHE_CONTROL,
					"X-Ipfs-Path": path,
					"Access-Control-Allow-Origin": "*",
				},
			});
		}

		const builtinRelease = ALL_RELEASES[cid];
		if (builtinRelease) {
			const targetFile = subpath || "subject.json";
			if (wantsCAR && !subpath) {
				const carText = JSON.stringify({
					version: 1,
					roots: [cid],
					files: builtinRelease.files,
				});
				return new Response(carText, {
					status: 200,
					headers: {
						"Content-Type": "application/vnd.ipld.car",
						"Content-Disposition": `attachment; filename="${cid}.car"`,
						"Cache-Control": IMMUTABLE_CACHE_CONTROL,
						"X-Ipfs-Path": path,
						"Access-Control-Allow-Origin": "*",
					},
				});
			}

			const seedContent = builtinRelease.files[targetFile];
			if (seedContent !== undefined) {
				const headers: Record<string, string> = {
					"Content-Type": detectContentType(targetFile),
					"Cache-Control": IMMUTABLE_CACHE_CONTROL,
					"X-Ipfs-Path": path,
					"Access-Control-Allow-Origin": "*",
				};
				if (targetFile.endsWith(".pdf")) {
					headers["Content-Disposition"] = `inline; filename="${targetFile}"`;
				}
				const response = new Response(seedContent, {
					status: 200,
					headers,
				});
				ctx.waitUntil(cache.put(request, response.clone()));
				return response;
			}
		}

		return new Response(JSON.stringify({ error: "CID or file not found", key: r2Key }), {
			status: 404,
			headers: {
				"Content-Type": "application/json",
				"Access-Control-Allow-Origin": "*",
			},
		});
	},
};
