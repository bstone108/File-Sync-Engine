# Permission Synchronization Policy

## Purpose

Permission handling must be configurable. Many deployments do not want peer file permissions copied exactly.

Examples:

- a central shared server where all users should be able to edit synced files;
- a restrictive server where files should be locked down regardless of source permissions;
- a platform where ACLs/ownership do not map cleanly across devices;
- a deployment that wants to ignore permissions and let OS defaults/umask decide.

## Folder-level policy

Each folder supports a permission policy similar to:

```jsonc
{
  "id": "shared-docs",
  "path": "/srv/shared-docs",
  "mode": "sendrecv",
  "permissions": {
    "mode": "default",        // ignore | sync | default | fixed
    "fileMode": "0666",      // used by fixed/default modes when applicable
    "dirMode": "0777",       // used by fixed/default modes when applicable
    "preserveOwner": false,
    "preserveGroup": false,
    "preserveACL": false
  }
}
```

The configuration schema is implemented for folder configs. Apply-time behavior is currently wired through the local folder sync path; peer/stream transfer permission mapping remains future protocol work.

## Modes

### `ignore`

Do not sync permissions from peers. Let the OS/filesystem defaults, process umask, and parent directory behavior determine created permissions.

### `sync`

Attempt to sync source permissions/metadata. This is optional and should not be the default for every deployment. It may be limited by platform support and user privileges.

Current implementation copies the source file's POSIX mode bits after staged content verification and atomic placement in the local sync path. Owner, group, and ACL preservation remain planned and platform-aware rather than silently claimed.

### `default`

Apply configured default permissions to created/repaired files and directories, but do not try to preserve peer-specific permissions exactly.

Useful for shared folders where newly synced files should be broadly editable.

Current implementation applies configured `fileMode`/`dirMode` when present. Empty mode fields leave the filesystem-created mode untouched.

### `fixed`

Force configured file/directory permissions after apply/repair, regardless of source permissions.

Useful for central servers that need either permissive shared-write behavior or restrictive locked-down behavior.

Current implementation applies configured `fileMode`/`dirMode` when present after verified local apply and directory creation.

## Safety rules

- Permission sync must be opt-in/configurable, not hardwired.
- Cross-platform behavior must be explicit: POSIX modes, Windows attributes/ACLs, ownership, and group handling are not equivalent.
- If ownership/group/ACL preservation fails due to privileges or platform support, report it through API/status rather than silently pretending it worked.
- Permission changes should happen after staged content is verified and atomically placed where possible.
- Do not let permission policy interfere with content verification.
