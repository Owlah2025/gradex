# Schema 43 Release Artifact Staging Evidence — 2026-09-23

This records the authorized non-serving artifact staging performed on
`srv1900125`. It is evidence of artifact availability, not deployment approval.

## Production state preserved

At `2026-09-23T16:39:17Z`, immediately before artifact imports, the selected
runtime revision was `98e88fcc1105e8c638bb638d3f1c46630bcc51b2`, and the live
schema marker was `41|false`. The production API, worker, frontend, and edge
containers were running; API and frontend health were `healthy`; all four
restart counts were zero. At `2026-09-23T16:44:29Z`, the selected revision and
schema were still unchanged, and the same production container IDs were running
with zero restarts. No staged artifact had a running container.

The import scripts verified archive checksums, manifest revision, OCI revision
labels, image IDs, backend schema ceiling, and release-specific migration or
tooling inventory checks. The schema-42 artifact was imported using the exact
`deploy/hostinger/import-release.sh` from frozen commit
`3383f46d0e9e6379c3bd166d39622c3659ae3d86` (script SHA-256
`3c873a9cf9167b19fd3760b86c7c1a9ed183474efa866a4eca7171cac51bb406`). Device
artifacts were imported with their checksum-verified `import-artifact.sh`.
Those scripts load images and create inactive release directories; none selects
a runtime release or starts an application service.

Host capacity after staging was 57 GiB free (`/dev/sda1`, 96 GiB total), with
Docker reporting 24.82 GB image storage. Staged directory modes and import
completion times (UTC) were:

| Revision | Role | Host release directory | Mode | Import completed |
| --- | --- | --- | --- | --- |
| `3383f46d0e9e6379c3bd166d39622c3659ae3d86` | schema 41→42 transition / clean-42 application | `/home/deploy/gradex-production/releases/3383f46d0e9e6379c3bd166d39622c3659ae3d86` | `0500` | `16:39:29` |
| `f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d` | reviewed schema-43 behavior base | `/home/deploy/gradex-production/releases/f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d` | `0700` | `16:40:06` |
| `54115fd6029d5d80af63640ac6f0bfe31be22d67` | schema-43 rollback-compatible application | `/home/deploy/gradex-production/releases/54115fd6029d5d80af63640ac6f0bfe31be22d67` | `0700` | `16:40:39` |
| `bb9d71b645fc1afbcf3666c5035c6b8396536ff2` | cleanup-capable schema-43 current application | `/home/deploy/gradex-production/releases/bb9d71b645fc1afbcf3666c5035c6b8396536ff2` | `0700` | `16:41:08` |

The current schema-41 recovery artifact remains at its pre-existing release
directory and was revalidated. SHA-256 values below are the release manifest
and image archive values on the production host. Schema-42 deploy-bundle hashes
are also recorded because its supervised boundary tooling is part of that
artifact.

| Revision | Manifest SHA-256 | Image archive SHA-256 | Deploy bundle SHA-256 | Backend image ID | Frontend image ID | Proof image ID | Backend max schema |
| --- | --- | --- | --- | --- | --- | --- | ---: |
| `98e88fcc1105e8c638bb638d3f1c46630bcc51b2` | `3aeeeb373f7a1cf898036a47b03cf614a8dc10d551af462dbabc3adc87200de3` | `e86244a8ad8cf699df55adfbc1a3cc8e796199f987cecb179c099562db091af4` | `16fbc72d4c9ae6e71b45e9e0eaf66aebc9dd3a4eb1672f682db3f371abd2bd0b` | `sha256:09d67492fc89394c8ce846659f1610dceffe76ab29a6684bea8c93c17c7c8ef2` | `sha256:8a78d60596eec881730c2171230a9a5d95d33bd8ddf034f98a428d8c7775dec1` | `sha256:d99af6c1c61e0e112474646e5fb9a85fc0a2d56b583fdb6233caff65ae3ff7e3` | 41 |
| `3383f46d0e9e6379c3bd166d39622c3659ae3d86` | `689820768687296bb97a7331a284f04dedf1b32422e7015e9e05f983a7eef58d` | `a6ec915b912f5e165f3b71a980c59d80ff08c66d56d05a9f5000b3f4e9c495d0` | `6598bdc2183722dc4fae3ed880f26f9c5d81fe194726362c30ceb330c26d2754` | `sha256:9f21f9b0d74cdc06601914c2f1db89a6f080843e81dea011697da494bbade9ae` | `sha256:66b2398ae74ff96833e15353ec4f6761ac6f05c124e0ab4656d3673e17163647` | `sha256:7dde5ca2549b8b92719f3c8bb2e586be5c16eac7b5d8e42d1421bddaf34983f9` | 42 |
| `f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d` | `baf5c8cd8c82b1d7e88ad6cf21637ffdeb8c47e27b5b7e1dfe4af44c2b4821bb` | `019a01fc2099240415a6575dd555f5f987006f7936598ced5f7ccd9acf2e12d0` | — | `sha256:07e3a560b6f131de169d5cc8135b50ba3150c197eb82db31ef5399b2e106032c` | `sha256:6948b66bd105cb73ae7fbab28640a0eeba2d73ee488cbc17db396d5f8a453e6f` | `sha256:9124dc370e86273a6d007df213749276ac8e607fbbf4b451c5179b63f3a01337` | 43 |
| `54115fd6029d5d80af63640ac6f0bfe31be22d67` | `22f4d3844a3ab691d79d9afcb399053bbfda91ce9cc83746c3a25d24a6e25b16` | `02dab2baf5322d61de24d94ce13adf613c305c2c64eb7d23aa4288df46cc679c` | — | `sha256:7a652d7b7c876b5470eb85a4f6c35d8cfa4650c8fc06f4009b5c75885d69146c` | `sha256:1620798c06f46de063e13cbe97fd9bf115c6b47693ef274682c630bf02c18255` | `sha256:572e426f2368156fa9ce95f8e65f3326d0efcd834190d0bd1fe65592161efdee` | 43 |
| `bb9d71b645fc1afbcf3666c5035c6b8396536ff2` | `c3889178f4fadcca18262c28945fbf340ff6d9446f20df6442eec471cfef27fa` | `1c63d83df591939c575e8b65bfd7a51e769c723fc924bc55fa854b196c793f3d` | — | `sha256:447f2e7b9c512a083347dee7d02887a9f5cb09a72b3d719b94f8a6d989126afb` | `sha256:99adeacfd0ec4ed035c17d3f6d9521b5e20ed0b0ac8664864ff19da4c339a55d` | `sha256:3a3177a86c0056095cb643f29dbb384c9d42f2e3184e1aedc20856dcb4205b8c` | 43 |

The 3383 artifact declares `SCHEMA42_CAPABILITY=manual-enhancement-v1`. The
schema-41 recovery artifact declares
`SCHEMA41_CAPABILITY=supervised-41-to-40-v1`. The rollback artifact identifies
3383 as its compatibility base and carries patch SHA-256
`9bd3e47288efbd8c78081dd2694ce94dea4fa3ba0c2525c460be6966266996f7`.
Artifacts f41, 54115, and the cleanup-capable candidate all contain the same
0043 migration hashes: up
`e98b04e775c9b0eb3997672e5bc92c8cc495371ac7fc4c181b89a552a5fc837f`, down
`1ab5b746a824327d5fdf5f6e39ce9d2ddae76d45bfd6acd0eebe42140f01529e`.

The cleanup-capable image was built from clean source revision
`bb9d71b645fc1afbcf3666c5035c6b8396536ff2`. Its `gradex-device-cutover -help`
output includes both `check-expired-staff-legacy` and
`apply-expired-staff-legacy`. It is needed because the original f41 artifact
does not contain these maintenance-only commands. The f41 artifact is retained
as the independently reviewed behavior base; the cleanup-capable artifact is
the operative schema-43 candidate for this maintenance sequence. The staged
image IDs were checked against both manifest IDs and OCI revision labels for
backend, frontend, and proof images.

The incoming archives were transferred to private revision-named directories,
checked against their SHA-256 sidecars, and imported using their release-owned
importers. All imported release paths contained no symlinks. A read-only Docker
inventory check found zero running containers from the staged 3383, f41,
54115, or cleanup-capable images.

This staging did not alter `runtime.env`, select a release, start/recreate an
application container, run a migration, or modify the live database. Production
still selected 98e88 on clean schema 41 after staging. No service was stopped or
restarted. These results must be rechecked immediately before maintenance; they
do not substitute for the fresh read-only preflight.
