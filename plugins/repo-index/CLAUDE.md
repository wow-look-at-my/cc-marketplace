# repo-index

A `UserPromptSubmit` hook. It matches the prompt against an index of the user's repositories and injects the link and description of each match, at most once per session.

- **The index is generated, never authored.** Every field comes from the GitHub API. The description is the repository's own or its README's first paragraph. The phrases are its name and its topics. Do not add a checked-in list of repositories, descriptions, or keywords. A hand-written index disagrees with reality the day after it is written. Nothing tells the reader when.
- **Two tiers decide a match. The split is the whole precision story.** An identifier (whole name, a two-word run of it, a topic) is worth 2. A term (a name part, or a rare word from the description) is worth 1. A repository needs 2. Naming a repository is enough. One shared English word is not, which is what stops "write a haiku" from suggesting quick-write-this-code.
- **Rarity picks the terms, measured across the index.** A word in at most 2% of descriptions identifies its repository. A word in more identifies nothing. This is why there is no list of interesting words to maintain, and why "xsd" finds xml-validator.
- **The hook never touches the network.** It reads the cache and, when that is due, starts `--refresh` as a separate process. The lock under the cache directory is time-based, so a crashed refresh frees itself.
- **A stale index still suggests. A missing one says so.** Serving week-old descriptions beats silence. Silence with no explanation is the failure.
- The hook never exits 2. Exit 2 blocks the user's prompt. A suggestion is not worth that.
