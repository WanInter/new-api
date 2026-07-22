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

`--capture-dir` recursively finds every `manifest.json` below the supplied
directory, so it also accepts `/archive/relay-captures` to merge the entire
archive. `--capture-root` remains an equivalent compatibility alias.

Every output line has the form `{"messages":[...]}`. Chat Completions exports
one line per response choice. Anthropic Messages and OpenAI Responses preserve
their structured content blocks. The JSONL output is created with mode `0600`
and is not overwritten unless `--force` is specified.

The utility supports the current `enc:v1` AES-256-GCM format with the
`relay-capture` purpose. Streaming, oversized, non-text, incomplete, or
malformed captures are skipped and reported on standard error. A missing or
different `CRYPTO_SECRET` causes decryption to fail.
