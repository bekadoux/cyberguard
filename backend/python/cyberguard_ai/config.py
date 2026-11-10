import os
import ssl
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit


def secret(name: str, default: str = '') -> str:
    direct = os.environ.get(name)
    secret_file = os.environ.get(f'{name}_FILE')
    if direct is not None and secret_file is not None:
        raise ValueError(f'{name}: configure either direct or _FILE')
    if secret_file:
        data = Path(secret_file).read_text(encoding='utf-8')
        return data.removesuffix('\n').removesuffix('\r')
    return direct if direct is not None else default


@dataclass(frozen=True)
class Settings:
    database_url: str
    go_base_url: str
    internal_token: str
    kev_base_url: str = 'http://127.0.0.1:8009'
    kev_model: str = 'kev-latest'
    llm_base_url: str = 'http://127.0.0.1:8081'
    llm_model: str = 'qwen2.5-1.5b-instruct'
    mode: str = 'models'  # 'fake' is ONLY a labelled integration test fixture
    playbooks_path: str = 'docs/playbooks.md'
    poll_seconds: float = 3.0
    lease_seconds: int = 900
    max_attempts: int = 3
    request_timeout: float = 180.0
    go_ca_file: str = ''
    require_tls: bool = False
    check_dependencies: bool = False

    def http_verify(self) -> ssl.SSLContext | bool:
        """Use explicit private-CA trust without enabling environment proxies."""
        if self.go_ca_file:
            return ssl.create_default_context(cafile=self.go_ca_file)
        return True

    @classmethod
    def from_env(cls) -> 'Settings':
        mode = os.environ.get('AI_MODE', 'models')
        if mode not in ('models', 'fake'):
            raise ValueError('AI_MODE must be models or fake')
        result = cls(
            database_url=secret('AI_DATABASE_URL'),
            go_base_url=os.environ.get('AI_GO_URL', 'http://127.0.0.1:8080').rstrip('/'),
            internal_token=secret('AI_INTERNAL_TOKEN'),
            kev_base_url=os.environ.get('AI_KEV_URL', 'http://127.0.0.1:8009').rstrip(
                '/'
            ),
            llm_base_url=os.environ.get('AI_LLM_URL', 'http://127.0.0.1:8081').rstrip(
                '/'
            ),
            kev_model=os.environ.get('AI_KEV_MODEL', 'kev-latest'),
            llm_model=os.environ.get('AI_LLM_MODEL', 'qwen2.5-1.5b-instruct'),
            mode=mode,
            playbooks_path=os.environ.get('AI_PLAYBOOKS_PATH', 'docs/playbooks.md'),
            poll_seconds=float(os.environ.get('AI_POLL_SECONDS', '3')),
            lease_seconds=int(os.environ.get('AI_LEASE_SECONDS', '900')),
            max_attempts=int(os.environ.get('AI_MAX_ATTEMPTS', '3')),
            request_timeout=float(os.environ.get('AI_REQUEST_TIMEOUT', '180')),
            go_ca_file=os.environ.get('AI_GO_CA_FILE', ''),
            require_tls=boolean_setting('AI_REQUIRE_TLS'),
            check_dependencies=boolean_setting('AI_CHECK_DEPENDENCIES'),
        )
        if not result.database_url or not result.internal_token:
            raise ValueError('AI_DATABASE_URL and AI_INTERNAL_TOKEN required')
        if not (30 <= result.lease_seconds <= 3600 and 1 <= result.max_attempts <= 10):
            raise ValueError('Invalid lease or attempts')
        if result.poll_seconds <= 0 or result.request_timeout <= 0:
            raise ValueError('Invalid timeout')
        if result.require_tls:
            result.validate_tls()
        return result

    def validate_tls(self) -> None:
        """Reject insecure cross-host settings without disclosing connection strings."""
        from psycopg.conninfo import conninfo_to_dict

        origin = urlsplit(self.go_base_url)
        if (
            origin.scheme != 'https'
            or not origin.hostname
            or origin.username is not None
            or origin.password is not None
            or origin.path not in ('', '/')
            or origin.query
            or origin.fragment
        ):
            raise ValueError('AI_GO_URL must be an HTTPS origin')
        try:
            connection = conninfo_to_dict(self.database_url)
        except Exception:
            raise ValueError('Invalid AI_DATABASE_URL') from None
        root_cert = connection.get('sslrootcert')
        if connection.get('sslmode') != 'verify-full' or not isinstance(root_cert, str):
            raise ValueError('AI_DATABASE_URL requires verify-full and sslrootcert')
        if not root_cert or not Path(root_cert).is_file():
            raise ValueError('Database CA file is missing')
        if (
            connection.get('user') != 'python_incidents'
            or connection.get('dbname') != 'cyberguard_incidents'
        ):
            raise ValueError(
                'Deployment requires python_incidents on cyberguard_incidents'
            )
        self.http_verify()


def boolean_setting(name: str) -> bool:
    """Read strict opt-in flags; a typo must not silently disable TLS checks."""
    value = os.environ.get(name, 'false')
    if value not in ('true', 'false'):
        raise ValueError(f'{name} must be true or false')
    return value == 'true'
