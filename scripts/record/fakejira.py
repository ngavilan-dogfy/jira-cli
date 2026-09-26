"""A tiny fake Jira for recording `jira setup` (scripts/record-setup.sh).

Serves serverInfo to anyone and, with basic auth ana@acme.com + the token given
on the command line, the few endpoints setup calls. Never used by tests.
Usage: fakejira.py PORT TOKEN
"""
import base64, json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
TOKEN = sys.argv[2]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body):
        b = body.encode() if isinstance(body, str) else json.dumps(body).encode()
        self.send_response(code); self.send_header('Content-Type', 'application/json'); self.end_headers(); self.wfile.write(b)
    def do_GET(self): self.route()
    def do_POST(self): self.route()
    def route(self):
        path = self.path.split('?')[0]
        if path == '/rest/api/3/serverInfo':
            return self.send(200, {"baseUrl": "https://acme.atlassian.net", "deploymentType": "Cloud"})
        auth = self.headers.get('Authorization', '')
        ok = False
        if auth.startswith('Basic '):
            u, _, p = base64.b64decode(auth[6:]).decode().partition(':')
            ok = (u == 'ana@acme.com' and p == TOKEN)
        if not ok:
            return self.send(401, "Client must be authenticated to access this resource.")
        if path == '/rest/api/3/myself':
            return self.send(200, {"accountId": "a1", "displayName": "Ana García", "emailAddress": "ana@acme.com", "active": True})
        if path == '/rest/api/3/project':
            return self.send(200, [{"id": "2", "key": "WEB", "name": "Website"}, {"id": "1", "key": "OPS", "name": "Operations"}])
        if path.startswith('/rest/api/3/search'):
            return self.send(200, {"issues": [], "isLast": True})
        return self.send(404, {"errorMessages": ["not found"]})
HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
