---
platform: HTB
machine: example
os: linux
services: [22/ssh, 80/http, 9100/jetdirect]
tags: [lpd, pjl, fsupload, privesc, user-flag]
outcome: user
anonymized: true
---

# Example engagement, printer job language

## Context

A Linux host exposes SSH, a web application and a printer service on 9100. The
host and the account names are anonymised on purpose: this document is a
pattern, not a write-up of one machine.

## Path

1. The web application discloses a maintenance page that references the printer
   queue.
2. The printer service accepts a job language request that returns a file from
   the filesystem.
3. The returned file contains the flag of the user level.

## What made it work

The service was reachable from the same network as the web application, and no
authentication was expected on the job submission endpoint.

## Reusable lesson

When a scan shows a printer protocol next to a web application, test whether the
printer shares a filesystem with the application host before attempting a web
exploit.
