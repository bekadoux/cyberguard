{
	admin off
	persist_config off
	auto_https disable_redirects
	http_port 8080
	https_port 8443
}

https://{$APP_PUBLIC_NAME}:8443 {
	import {$APP_TLS_HANDLER:Caddyfile.acme}
	import routes.caddy
}

http://:8080 {
	redir https://{$APP_PUBLIC_AUTHORITY}{uri} permanent
}

# Container-only health listener, never published on the host.
http://:9080 {
	respond /__gateway_health "ok" 200
	respond 404
}
