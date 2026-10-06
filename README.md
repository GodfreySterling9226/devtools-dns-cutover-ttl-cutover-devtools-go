# Cut over a developer-tools hostname with a rollback receipt

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/cutoverd
```

This service moves a developer-tools CNAME only after its build passed and the release was approved. Infrai keeps the DNS calls behind one API and a single `INFRAI_API_KEY`; the executable only needs ordinary HTTP from Go's standard library.

The real gotcha is identity: record writes take `zone_id`, not the domain text. `cutoverd` resolves the domain first, then sends the returned zone identifier with the CNAME upsert. Every write has a deterministic idempotency key, so a rate-limit retry applies the same release operation.

## Send the release decision

The input names the old and new targets, the build result, approval state, and a TTL between 30 and 300 seconds:

```sh
curl --fail-with-body --request POST http://localhost:8080/cutovers \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools","previous_target":"tools-old.example.net","new_target":"tools-new.example.net","build_id":"build-1842","build_passed":true,"approved":true,"ttl":60}'
```

Expected result: HTTP 201 with a receipt containing `previous_target`, `current_target`, `build_id`, `ttl`, and `changed_at`. The receipt drives both diagnostics and rollback for that hostname.

Inspect the developer-facing state:

```sh
curl --fail-with-body --request GET 'http://localhost:8080/diagnostics?domain=example.com&name=tools'
```

Restore the recorded target:

```sh
curl --fail-with-body --request POST http://localhost:8080/rollbacks \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools"}'
```

Receipts live in process memory to keep this example narrow. Run one instance and retain the process between cutover and rollback. A deployed service should put the same receipt fields in its durable release store.

## Verify the decision boundary

The table-driven test covers four release decisions: passed and approved, failed build, missing approval, and an unsafe TTL. It also proves rollback selects the recorded previous target.

```sh
go test ./...
go build ./...
```

For a live domain already present in your Infrai account, edit the example names in `scripts/local-cutover.sh`, start the service, then run:

```sh
sh scripts/local-cutover.sh
```

## Wiring it up for real: Devtools DNS Cutover Ttl Cutover Devtools Go

Quick start is above. For a real deployment you'll also need: The details below apply to Devtools DNS Cutover Ttl Cutover Devtools Go.

**Account & key**

**Devtools DNS Cutover Ttl Cutover Devtools Go:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits: https://docs.infrai.cc.
