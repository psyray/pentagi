---
name: ssrf-out-of-band
description: Confirm and exploit a server-side request forgery with an out-of-band callback.
tags: [ssrf, oob, web]
---

# SSRF out of band

## When to use

A parameter takes a URL, a hostname or an image source and the response does
not show the fetched content.

## Pre-flight checks

- Prepare a listener reachable from the target.
- Note every parameter that accepts a URL: webhooks, avatars, imports, PDF
  generators, link previews.

## Steps

1. Point the parameter at the listener and watch for a request.
2. If the target resolves names, try an internal hostname to map the network.
3. When the body is reflected, target the metadata endpoint of the platform.

## Safe verification

A callback on the listener proves the request left the application. Do not use
the forgery against internal services that could change state.

## Cleanup

Stop the listener and clear the external references you created.
