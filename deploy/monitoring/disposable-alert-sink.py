#!/usr/bin/env python3

import json
import os
import pathlib
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


if len(sys.argv) != 3:
    raise SystemExit("usage: disposable-alert-sink.py PORT_FILE OUTPUT_FILE")

port_file = pathlib.Path(sys.argv[1])
output_file = pathlib.Path(sys.argv[2])
expected_token = os.environ.get("SINK_EXPECTED_TOKEN", "")


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802
        if self.path != "/alerts":
            self.send_error(404)
            return
        if expected_token and self.headers.get("Authorization") != f"Bearer {expected_token}":
            self.send_error(401)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.send_error(400)
            return
        if length < 1 or length > 65536:
            self.send_error(413)
            return
        try:
            payload = json.loads(self.rfile.read(length))
        except (UnicodeDecodeError, json.JSONDecodeError):
            self.send_error(400)
            return
        with output_file.open("a", encoding="utf-8") as sink:
            sink.write(json.dumps(payload, separators=(",", ":")) + "\n")
        self.send_response(202)
        self.end_headers()

    def log_message(self, _format, *_args):
        return


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
port_file.write_text(str(server.server_address[1]), encoding="ascii")
server.serve_forever()
