#!/bin/sh
set -eu

curl --fail-with-body --request POST http://localhost:8080/cutovers \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools","previous_target":"tools-old.example.net","new_target":"tools-new.example.net","build_id":"build-1842","build_passed":true,"approved":true,"ttl":60}'

curl --fail-with-body --request GET 'http://localhost:8080/diagnostics?domain=example.com&name=tools'

curl --fail-with-body --request POST http://localhost:8080/rollbacks \
  --header 'Content-Type: application/json' \
  --data '{"domain":"example.com","name":"tools"}'
