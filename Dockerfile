# The whole app in one image: the web build embedded in the Go server, plus
# the migrate, seed and prune commands. Run it with compose.yaml (make docker-up).

# 1. Web build (react-router build, prerendering the public pages).
FROM node:24-alpine AS web
WORKDIR /src/apps/web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci
COPY apps/web ./
# Baked into the build as the product name (vite.config.ts).
ARG APP_NAME=lewcontext
RUN APP_NAME="$APP_NAME" npm run build

# 2. Go binaries, with the web build embedded (as scripts/build-web.sh does).
FROM golang:1.26-alpine AS api
WORKDIR /src/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api ./
COPY --from=web /src/apps/web/build/client ./internal/web/dist
RUN rm -rf internal/web/dist/.vite \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/... \
  && mv /out/server /out/api

# 3. Runtime: the binaries only, as a non-root user.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
COPY --from=api /out/ /usr/local/bin/
USER app
# The same product name as the web build, for the server's emails.
ARG APP_NAME=lewcontext
ENV API_ADDR=0.0.0.0:3000 APP_NAME=$APP_NAME
EXPOSE 3000
CMD ["api"]
