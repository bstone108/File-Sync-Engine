# Identity-linked config sync

Engines that share an identity keep a replica of one another's configuration. The replica is a store for remote management. Each instance owns its own settings. A GUI signed in to any linked engine can read and change every linked engine, including one that is offline. The change is queued for that engine and moves through whichever linked engine is online until the target applies it to its own record.

Changing a setting on instance 1 stores instance 1's record on instance 2 so a GUI there can view and manage instance 1. It does not change that setting on instance 2.

## Records and clocks

Records are keyed by instance UUID. There is no shared namespace. A setting name such as `logging.level` lives inside exactly one instance's record. Paths that try to address another instance, including `shared/...` and `instance/<id>/...`, are rejected.

An engine applies only its own record. The copy it holds of another instance is stored and is never written into the local record or the local config file. A cached copy of an engine's own record that comes back from a peer is not adopted either. The owner is the only engine that writes its record.

Inside one instance's record, each field has a hybrid logical clock: wall time in milliseconds, a counter, and the writer instance id. Merge keeps the later clock. Different fields of the same record both survive. When the wall time and counter match, the higher instance id wins, so two stored copies of that record resolve the same way no matter which engine merges first. A stale field does not discard the other fields in the record.

## Connections and queued edits

Each engine publishes its own connection record: label, API URL, addresses, online flag, and last seen. The record does not contain API keys, private keys, or other secrets. The sync path rejects fields whose name or value looks like a credential.

An edit aimed at instance X is stored as an X-targeted pending edit. The origin and any relay keep that edit in the queue. They do not write it into X's stored record and they do not apply it locally. Until X merges a replica that contains the edit, the status stays `pending`. X applies the edit to its own record when the edit's clock is newer than the field it already has, and then publishes that record. An older clock does not replace a newer field on X. Any online engine that merges the replica keeps pending edits for engines other than itself. That is the relay. X does not have to be a direct peer of the GUI that queued the edit.

## What the GUI shows

`GET /v1/config-sync` lists every known instance. The local engine is first. Each row has online or offline, last seen, the number of pending edits still waiting for that engine, `averageBytesPerSecond` (recent average data rate with that peer), `pendingBytes` (bytes still waiting to sync), and the stored settings for that engine. `GET /v1/peers` includes the same two transfer fields for each configured peer. The numbers come from observed block transfers, not from the replicated settings record. The GUI opens on the local engine. Choosing another engine shows that engine's stored record. `POST /v1/config-sync/edit` queues an edit for the selected instance. When the selected instance is the local engine, the edit is applied to the local record and the local config. When it is another engine, the local config is left unchanged.

`POST /v1/onboarding` asks one instance to issue or accept a one-time pairing code. The selected instance is the only engine that generates the code or links the new device. A request for the local engine runs immediately. A request for any other instance, including one that is offline, stays a pending onboarding command in the replica. Linked engines relay that command. The target runs it when it is online, then publishes the result: the issued code, or the accept outcome with the code removed. The command is not a settings field and is not applied to any other engine's config.

Paired engines exchange replicas with `POST /v1/config-sync/merge` whenever a link refresh reaches a peer. The request body is the caller's replica. The response is the peer's merged replica, which the caller saves. A peer that is down is skipped; the next online peer still carries the pending edits.

The browser never receives an API key. Connection records and edit values are rejected when they contain one.
