// The network half: reading files out of a GitHub repository at one pinned
// commit. This is the entire network surface, so a test supplies its own Client
// and never touches the network.

import type { Upstream } from "./transform.ts";

export interface Client {
  /**
   * Turns a branch name into the commit it points at right now, so every file
   * in one run comes from the same tree and the recorded provenance is exact.
   */
  resolve(src: Upstream): Promise<string>;
  /** Reads one file from a repository at a pinned commit. */
  get(repo: string, commit: string, path: string): Promise<string>;
}

const API = "https://api.github.com";

/**
 * The GitHub REST client. It sends a token when one is in the environment:
 * unauthenticated requests are limited to 60 per hour, and one run reads more
 * files than that once includes are counted.
 */
export class GitHubClient implements Client {
  #cache = new Map<string, string>();

  #headers(accept: string): HeadersInit {
    const headers: Record<string, string> = {
      Accept: accept,
      "User-Agent": "cc-marketplace-vendor-docker-docs",
      "X-GitHub-Api-Version": "2022-11-28",
    };
    const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
    if (token) headers.Authorization = `Bearer ${token}`;
    return headers;
  }

  async #request(url: string, accept: string): Promise<Response> {
    const response = await fetch(url, { headers: this.#headers(accept) });
    if (response.ok) return response;

    // A rate limit reads as a permissions problem unless it is named, and it is
    // the failure an unauthenticated run actually hits.
    const remaining = response.headers.get("x-ratelimit-remaining");
    const limited = response.status === 403 && remaining === "0";
    const hint = limited
      ? " (GitHub rate limit exhausted; set GITHUB_TOKEN or GH_TOKEN)"
      : "";
    throw new Error(`GET ${url} -> ${response.status} ${response.statusText}${hint}`);
  }

  async resolve(src: Upstream): Promise<string> {
    const response = await this.#request(
      `${API}/repos/${src.repo}/commits/${src.ref}`,
      "application/vnd.github+json",
    );
    const { sha } = (await response.json()) as { sha?: string };
    if (typeof sha !== "string" || sha.length !== 40) {
      throw new Error(`${src.repo}: expected a 40-character commit for ${src.ref}, got ${JSON.stringify(sha)}`);
    }
    return sha;
  }

  /**
   * Results are cached because several pages include the same partial, and each
   * cache miss is a network round trip.
   */
  async get(repo: string, commit: string, path: string): Promise<string> {
    const key = `${repo}@${commit}/${path}`;
    const hit = this.#cache.get(key);
    if (hit !== undefined) return hit;

    const response = await this.#request(
      `${API}/repos/${repo}/contents/${path}?ref=${commit}`,
      "application/vnd.github.raw",
    );
    const body = await response.text();
    this.#cache.set(key, body);
    return body;
  }
}
