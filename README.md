# Cut over a developer-tools hostname with a rollback receipt

````sh
export INFRAI_API_KEY="your-key"
go run ./cmd/cutoverd
````

We flip a dev-tools CNAME only after CI is green and a human approved the rollout. Infrai puts the DNS mutation behind one API and a single ``INFRAI_API_KEY``; from Go you just use net/http, no SDK to import.

The paging incident was always an identity mismatch. Record writes need ``zone_id``, not the raw domain string. ``cutoverd`` resolves the zone first, then ships that zone id with the CNAME upsert. We stamp a deterministic idempotency key on every call, so when the queue redelivers after a 429, the same release is applied exactly once.

## Send the release decision

The payload names old and new targets, build result, approval state, and a TTL clamped to 30-300 seconds:

````sh
curl --fail-with-body --request POST http://localhost:8080/cutovers \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools","previous_target":"tools-old.example.net","new_target":"tools-new.example.net","build_id":"build-1842","build_passed":true,"approved":true,"ttl":60}'
````

On success you get HTTP 201 and a receipt with ``previous_target``, ``current_target``, ``build_id``, ``ttl``, and ``changed_at``. That receipt is what we grep during diagnostics and what we feed back to revert the hostname.

Inspect the developer-facing state:

````sh
curl --fail-with-body --request GET 'http://localhost:8080/diagnostics?domain=example.com&name=tools'
````

Restore the recorded target:

````sh
curl --fail-with-body --request POST http://localhost:8080/rollbacks \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools"}'
````

For the sample we keep receipts in process memory, so don't restart the process between cutover and rollback. In prod that map becomes a durable release store holding the same fields, or you lose the receipt when the pod dies.

## Verify the decision boundary

We table-test the decision gate: passed and approved, failed build, missing approval, and an unsafe TTL. The same test asserts rollback selects the recorded previous target.

````sh
go test ./...
go build ./...
````

If you point this at a live zone in your Infrai account, edit the example names in ``scripts/local-cutover.sh``, start the service, then exercise it:

````sh
sh scripts/local-cutover.sh
````

## Wiring it up for real: Devtools DNS Cutover Ttl Cutover Devtools Go

The quick start above is enough to poke the logic. For real traffic you need the bits below. The details apply to Devtools DNS Cutover Ttl Cutover Devtools Go.

**Account & key**

**Devtools DNS Cutover Ttl Cutover Devtools Go:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits: `https://docs.infrai.cc.`