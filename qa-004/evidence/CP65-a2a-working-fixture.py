"""Loopback-only deterministic endpoint for reproducing QA-004 DEF-067."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        body = json.dumps({
            "name": "QA004 Cancel Lifecycle Agent",
            "version": "1.0.0",
            "url": "http://127.0.0.1:18133",
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(length)
        body = json.dumps({
            "state": "working",
            "spent_micros": 0,
            "artifacts": [],
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

HTTPServer(("127.0.0.1", 18133), Handler).serve_forever()
