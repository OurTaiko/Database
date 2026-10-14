FROM node:24-alpine AS build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginx:1.29-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
# Writable paths live in the tmpfs mounted at /tmp.
RUN sed -i 's#/var/run/nginx.pid#/tmp/nginx.pid#' /etc/nginx/nginx.conf \
    && sed -i '/^http {/a\    client_body_temp_path /tmp/client_body;\n    proxy_temp_path /tmp/proxy;\n    fastcgi_temp_path /tmp/fastcgi;\n    uwsgi_temp_path /tmp/uwsgi;\n    scgi_temp_path /tmp/scgi;' /etc/nginx/nginx.conf
EXPOSE 8080
