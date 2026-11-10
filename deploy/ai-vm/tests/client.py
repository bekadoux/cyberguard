"""Credential-redacted client for disposable Go/TLS/AI deployment acceptance."""

import ssl
import sys
from pathlib import Path

import httpx


def main() -> None:
    context = ssl.create_default_context(cafile='/run/certs/ca.crt')
    with httpx.Client(verify=context, trust_env=False, timeout=10) as client:
        response = client.post(
            'https://app-tls:8443/auth/login',
            json={
                'username': 'employee_a' if sys.argv[1] == 'create' else 'analyst',
                'password': Path(
                    '/fixture/employee-password'
                    if sys.argv[1] == 'create'
                    else '/fixture/analyst-password'
                )
                .read_text()
                .strip(),
            },
        )
        assert response.status_code == 200, f'login_status_{response.status_code}'
        cookie = '; '.join(f'{key}={value}' for key, value in client.cookies.items())
        if sys.argv[1] == 'create':
            response = client.post(
                'https://app-tls:8443/incidents',
                json={
                    'title': 'DISPOSABLE deployment fixture',
                    'description': 'Synthetic transport/worker check, not AI evaluation.',
                },
            )
            assert response.status_code == 201, f'create_status_{response.status_code}'
            incident = response.headers['location'].rstrip('/').split('/')[-1]
            # Also check employee denial using the same actual Go session.
            response = client.get(
                f'https://ai-gateway:8443/incidents/{incident}/assessment',
                headers={'Cookie': cookie},
            )
            assert response.status_code == 403, f'employee_status_{response.status_code}'
            print(incident)
        else:
            response = client.get(
                f'https://ai-gateway:8443/incidents/{sys.argv[2]}/assessment',
                headers={'Cookie': cookie},
            )
            assert response.status_code == 200, f'read_status_{response.status_code}'
            body = response.json()
            assert body['job']['status'] == 'completed'
            assert (
                'SYNTHETIC DEPLOYMENT FIXTURE'
                in body['result']['assessment']['brief']['summary']
            )
            print('Stored synthetic fixture retrieved through TLS and Go authorization.')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(
            'Deployment client check failed: '
            + (str(error) if isinstance(error, AssertionError) else type(error).__name__)
            + '; sensitive diagnostics suppressed.',
            file=sys.stderr,
        )
        sys.exit(1)
