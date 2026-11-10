FROM node:24.22.2-bookworm-slim@sha256:01d566e8d94d8c619591d78652babfccd3c6992a3dffa9ed5ab39e11cf6c70e9 AS build
WORKDIR /app
RUN npm install --global npm@12.2.2
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
# Deliberate inputs: never copy host .env files, build output or local LAN overrides.
COPY frontend/index.html frontend/tsconfig.json frontend/vite.config.ts ./
COPY frontend/src/ ./src/
COPY frontend/tests/ ./tests/
COPY frontend/public/ ./public/
RUN npm run build

FROM caddy:2-alpine@sha256:78bd35025c7ba85961dd149ae670315129954e77abe2383ac47c89106b35ade9
COPY docker/caddy/ /etc/caddy/
COPY --from=build /app/dist/ /srv/
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --retries=6 CMD wget -q --spider http://127.0.0.1:8080/__gateway_health || exit 1
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
