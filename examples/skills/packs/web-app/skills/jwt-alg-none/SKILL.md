---
name: jwt-alg-none
description: Verify whether a JSON Web Token flow accepts the none algorithm, then demonstrate impact.
tags: [jwt, auth, web]
---

# JWT alg=none

## When to use

The application issues JSON Web Tokens and the verification library or the
framework version is unknown.

## Pre-flight checks

- Capture a valid token and note the algorithm in the header.
- Confirm the claim that drives authorisation (role, scope, tenant).

## Steps

1. Re-encode the header with `"alg":"none"` and drop the signature.
2. Replay the token on an endpoint that checks a privileged claim.
3. If the token is accepted, request the privileged resource to prove impact.

## Safe verification

Acceptance of the forged token on a read-only endpoint is enough. Do not change
data or escalate to another account without written authorisation.

## Known limitations

Modern libraries reject `none` by default; a negative result only rules out
this specific bypass.
