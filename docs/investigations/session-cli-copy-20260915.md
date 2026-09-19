# Session CLI copy failure (2026-09-15)

## Failure mechanism

The compatibility image `ghcr.io/ccplant/ccplant-backend:v0.3.191`
stores the CLI at `/opt/ccplant/bin/ccplant`; `/usr/local/bin/ccplant`
is a symlink to that file. Mounting the installation emptyDir at
`/opt/ccplant/bin` in the init container hides the source binary.
The original copy command then fails with:

```
cp: cannot stat '/usr/local/bin/ccplant': No such file or directory
```

Mount the destination at `/ccplant-cli` in the init container. Keep the
main container's read-only mount at `/opt/ccplant/bin` so runtime paths
remain consistent.

## Verification

On September 15, disposable Pods using the production compatibility image,
UID/GID 999, fsGroup 999, and an emptyDir reproduced the original copy
failure. The corrected mount and copy command successfully copied the CLI,
applied mode 0555, and executed `ccplant --help`.

At investigation time, the production manager already explicitly selected
`ghcr.io/ccplant/ccplant-api:v0.3.191` as its CLI image, which avoids this
symlink problem. This investigation did not change that live setting.
Historical failed Pod logs were unavailable, so this reproduction alone
does not establish the cause of the originally reported outage.

Two disposable sessions using the user's default profile were created:
one through the Fly API and one through authenticated
`https://app.ccplant.com/api/v1/start`. Both reached agent status `stable`;
the public frontend proxy also returned their status successfully.
Deletion through the public API removed both session records and their
allocated Pods, and a ready idle runner remained available. No initial
agent message was sent. The reproduction Pods were also removed.

The source fix requires a subsequent manager release/deployment. These
live checks validate the existing API-image workaround, not deployment
of the source fix.
