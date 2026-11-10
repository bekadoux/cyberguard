"""Synthetic model responses for the disposable deployment smoke, NEVER real inference."""

import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


class Handler(BaseHTTPRequestHandler):
    """Expose readiness and bounded, explicitly synthetic model responses."""

    def log_message(self, format: str, *args: object) -> None:
        pass

    def reply(self, body: dict, status: int = 200) -> None:
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def do_GET(self) -> None:
        if not Path(f'/control/{sys.argv[1]}-ready').exists():
            self.reply({}, 503)
        elif self.path == '/v1/models':
            self.reply({'models': [{'name': 'kev-latest'}]})
        else:
            self.reply({'status': 'ok'})

    def do_POST(self) -> None:
        # Discard, never log or echo incident input.
        self.rfile.read(int(self.headers.get('Content-Length', '0')))
        if self.path == '/v1/systemone':
            self.reply(
                {
                    'answers': {
                        'category': {'probabilities': {'other': 1.0}},
                        'impact': {'score': 0.0},
                        **{
                            name: {'noul': 0.0}
                            for name in (
                                'clicked_suspicious_link',
                                'credentials_entered',
                                'files_encrypted',
                                'unauthorized_access',
                            )
                        },
                    }
                }
            )
        else:
            self.reply(
                {
                    'choices': [
                        {
                            'message': {
                                'content': json.dumps(
                                    {
                                        'summary': (
                                            'SYNTHETIC DEPLOYMENT FIXTURE — '
                                            'not AI inference.'
                                        ),
                                        'reported_facts': [],
                                        'unknowns': [],
                                        'follow_up_questions': [],
                                    }
                                )
                            }
                        }
                    ]
                }
            )


if __name__ == '__main__':
    ThreadingHTTPServer(('0.0.0.0', int(sys.argv[2])), Handler).serve_forever()
