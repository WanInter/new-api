# Relay Capture Offline JSONL Exporter

`relay-capture-decrypt` is a standalone utility that exports copied relay
capture artifacts as OpenAI-compatible JSONL conversations. It does not connect
to a running new-api instance, database, or network service.

Build a portable Linux binary from the repository checkout:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o relay-capture-decrypt ./cmd/relay-capture-decrypt
```

Transfer only the binary and copied capture artifacts to the offline host.
Obtain the exact stable `CRYPTO_SECRET` through the approved secret-management
process. Do not put it in a command argument, file, or shell history.

Export all capture artifacts beneath a channel, day, or archive directory as JSONL:

```bash
read -r -s -p 'CRYPTO_SECRET: ' CRYPTO_SECRET; echo
export CRYPTO_SECRET

./relay-capture-decrypt \
  --capture-dir /archive/relay-captures/2026/07/22/channel-1 \
  --output /archive/conversations.jsonl

unset CRYPTO_SECRET
```

`--capture-dir` recursively finds legacy directories containing `manifest.json`
and S3 segment archives ending in `.tar.gz.enc`, so it also accepts
`/archive/relay-captures` to merge the entire archive. `--capture-root` remains
an equivalent compatibility alias. For segmented S3 storage, copy the archive
objects; the accompanying `.index.enc` files are used by the live service and
are not needed by this exporter.

A segment archive contains multiple capture directories in a tar stream. The
individual `request` and `response` entries are not separately compressed; the
complete tar stream is gzip-compressed and then AES-256-GCM encrypted with the
`relay-capture-segment` purpose.

Every output line has the form `{"messages":[...]}`. Chat Completions exports
one line per response choice. Anthropic Messages and OpenAI Responses preserve
their structured content blocks for non-streaming captures. For supported text
SSE captures, the exporter reconstructs the assistant text from the client-
visible stream events. The JSONL output is created with mode `0600` and is not
overwritten unless `--force` is specified.

The utility supports the current `enc:v1` AES-256-GCM format with the
`relay-capture` purpose for legacy single-object captures and the
`relay-capture-segment` purpose for segment archives. It also handles legacy
per-part gzip compression when it is declared in `manifest.json`. Oversized,
non-text, incomplete, malformed, or non-textual streaming captures are skipped
and reported on standard error. A missing or different `CRYPTO_SECRET` causes
decryption to fail.
