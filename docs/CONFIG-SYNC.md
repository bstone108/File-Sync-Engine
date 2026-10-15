# Identity-linked config sync

Engines that share an identity keep one replicated config database. A GUI signed in to any of them can read and change every linked engine, including one that is offline. The change waits in the database and moves through whichever linked engine is online until the target applies it.

## Clocks and namespaces

Each field has a hybrid logical clock: wall time in milliseconds, a counter, and the writer instance id. Merge keeps the later clock. The same field edited on two offline engines resolves to the later clock. Different fields both survive. When the wall time and counter match, the higher instance id wins, so the result is the same no matter which engine merges first.

Rows are namespaced so engines do not collide:

- `shared/<key>` is group configuration, such as a logging level every engine should adopt.
- `instance/<instance-id>/<key>` belongs to one engine. Two engines can both store `folders.docs.path` because the instance id is part of the key.

This is last-writer-wins per field, with the hybrid clock as the writer order. It is not a whole-document revision. A stale edit does not discard the other fields in the same replica.

## Connections and offline edits

Each engine publishes its own connection record: label, API URL, addresses, online flag, and last seen. The record does not contain API keys, private keys, or other secrets. The sync path rejects fields whose path or value looks like a credential.

An edit aimed at another engine is stored as a pending edit plus the field value. Until the target merges a replica that contains that edit, the status stays `pending`. Any online engine that merges the replica keeps pending edits for engines other than itself. That is the relay. The target does not have to be a direct peer of the origin. The next merge with any engine that has seen the edit delivers it. An older clock does not replace a newer field on the target.

## What the GUI shows

`GET /v1/config-sync` lists every known instance. The local engine is first. Each row has online or offline, last seen, and the number of pending edits still waiting for that engine. The GUI opens on the local engine. Choosing another engine queues edits for it through `POST /v1/config-sync/edit` when it is offline.

Paired engines exchange replicas with `POST /v1/config-sync/merge` whenever a link refresh reaches a peer. The request body is the caller's replica. The response is the peer's merged replica, which the caller saves. A peer that is down is skipped; the next online peer still carries the pending edits.

The browser never receives an API key. Connection records and edit values are rejected when they contain one.
