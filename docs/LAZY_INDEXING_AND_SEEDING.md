# Lazy Indexing and Manual Seeding

## Purpose

The sync engine must avoid Syncthing-style blocking index behavior. A newly added folder that already contains files should become usable quickly after a cheap metadata pass, then verify block hashes lazily without blocking all receiving/sending behavior.

This is especially important for manual seeding:

1. Copy files into place by external means.
2. Add/connect the folder to the sync engine.
3. Download or receive the authoritative remote database/index from a good peer.
4. Assume matching local files are correct when cheap metadata matches, but remember their block hashes are not locally verified yet.
5. Verify hashes later on demand or in an idle/background task.

## Two-stage local indexing

### Stage 1: cheap metadata pass

The first pass should collect file records without hashing every block:

- relative path
- size
- modification time
- creation/birth time where available
- platform metadata needed for change detection
- hash status: `unknown`

This pass is allowed to be broad and fast. It should not block sync startup on hashing every byte of every file.

### Stage 2: lazy block hashing

Block hashing is a lazy index task. It should run:

- on demand when another peer asks this node to send blocks for an unverified file;
- on demand when a local operation needs trustworthy blocks for reuse;
- in the background when the daemon is otherwise idle or below configured resource limits.

The block index/database should be the normal source for known block lookups. Do not rescan and rehash every file on every sync decision.

Current prototype status: the engine stores quick metadata records with `hashState: "unknown"`, can hash a single indexed file on demand when its cheap metadata still matches the quick baseline, and has a stable-order `HashNextUnknown` primitive suitable for idle background work. Manual seed adoption can now import an authoritative peer manifest for a size-matching quick-indexed local file as `hashState: "assumed-valid-unverified"`, preserving the local quick-index baseline separately from authoritative blocks/dates. First lazy verification refuses locally changed baselines, verifies local block hashes against the authoritative expected blocks, then applies the authoritative modification time and marks the manifest complete. The control API now exposes per-folder quick/lazy/verified index state, queued hash counts, provisional seeded-file read-only state, pending authoritative date correction counts, and typed hash/repair progress event fields. Background daemon queue wiring and block repair from peers remain planned.

## Trust model for seeded files

When a remote authoritative database says a file should have a given size/mtime/ctime and the local cheap metadata matches, the engine may treat the local file as provisionally present even if local block hashes are not known yet.

The local DB should record something equivalent to:

```text
file path: docs/report.pdf
size/mtime/ctime: matched
block hashes: unknown locally, expected from authoritative peer index
verification state: assumed-valid-unverified
```

Until the first local hash verification completes, the remote peer's database/index is treated as authoritative for expected block hashes **and authoritative file dates/metadata**.

## Authoritative file dates

For seeded/pre-existing files, the authoritative database owns the file dates and metadata once that file is accepted as matching the authority. That includes modification time, creation/birth time where the platform supports it, and other stored timestamp metadata.

Before a file is hashed for the first time, local file dates are used only as a change-detection data point against the cheap metadata baseline. They are not treated as the final desired dates.

On first successful hash verification:

1. verify content against the authoritative expected blocks;
2. if content matches, set local file dates/metadata to the authoritative database values;
3. mark the file verified;
4. if content differs, repair bad blocks from a good peer, then set dates/metadata to the authoritative database values after staged repair succeeds.

If local size/mtime/ctime/birth time changes after the initial full-tree cheap metadata baseline and before first hash verification, treat that as an intentional local change that can supersede the database according to folder mode/conflict policy. If dates differ from the authoritative database but have not changed since the baseline, treat the file as out of compliance with the authoritative copy and correct dates during first verification/repair rather than treating those date differences as local edits.

## Read-only while hashing/verification rule

For files that existed before the first cheap metadata pass and are adopted from an existing remote database with unknown local hashes:

- expose a read-only / verification-in-progress flag via API;
- embedding software should know that edits to these files are not authoritative until verification completes;
- if a pre-existing unverified file is changed after the cheap metadata baseline, that local change should not be advertised as valid content until the prior baseline has been verified or conflict policy explicitly resolves it;
- once the quick metadata baseline is complete, deletes and newly created files can be treated as intentional local actions because they happened after the baseline.

The phrase "read-only while hashing" here means sync-engine policy/readiness state, not necessarily OS-level file permissions in the first implementation.

## On-demand proof

If another peer requests blocks from an unverified local file, hash that file or the requested blocks before sending. Do not send assumed-correct bytes as if they are verified.

If background/on-demand hashing discovers the local file differs from the authoritative expected hash:

1. mark the differing local blocks bad/untrusted;
2. fetch replacement blocks from a good peer;
3. repair through the normal staged/verified apply path;
4. emit API events so embedding software can surface corruption/repair state.

## Change detection

The quick metadata pass establishes the initial baseline. Later filesystem changes are detected by metadata/watch events:

- if path/size/mtime/ctime changes after the baseline, the engine knows the file changed locally;
- if file dates differ from the authoritative database but did **not** change after the baseline, correct those dates to the database values during first verification/repair;
- new files created after the baseline are local changes even before block hashing finishes;
- files deleted after the baseline are local deletes;
- pre-existing unverified files should remain provisional until hashed or repaired.

## API visibility target

The realtime/control API should expose at least:

- folder indexing mode: `quick-metadata`, `lazy-hashing`, `verified`, `repairing`;
- whether authoritative date/metadata correction is pending for seeded files;
- counts for total files, known block hashes, unknown/unverified files, bad blocks, queued hash jobs, active hash jobs;
- read-only/provisional flag while seeded files are still unverified;
- events for hash queued/started/finished, mismatch found, repair started/finished.

## Implementation notes

- The durable block index should be an actual metadata DB, not repeated full rescans.
- The scanner should have modes: metadata-only, per-file hash, requested-block hash, background idle hash.
- First verification/repair should apply authoritative database file dates/metadata when the file was not changed after the quick metadata baseline.
- Peer pull planning should consult the block index first, then request/hash only missing or unknown blocks.
- The existing JSON store can model this temporarily, but production should move to the durable embedded DB planned in the roadmap.
