#!/usr/bin/env python3
"""Minimal HTTP listener that prints the request it receives.

Usage: python3 ssrf_probe.py 8081

A callback here proves that the target issued the request you asked for.
"""

import http.server
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        print(f"callback: {self.command} {self.path} from {self.client_address[0]}", flush=True)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *args):
        pass


def main() -> None:
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8081
    server = http.server.HTTPServer(("0.0.0.0", port), Handler)
    print(f"listening on 0.0.0.0:{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
