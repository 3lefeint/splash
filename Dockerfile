# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# The build writes into ../backend/internal/webui/dist
RUN mkdir -p /src/backend/internal/webui/dist && npm run build

FROM golang:1.24-alpine AS backend
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/backend/internal/webui/dist ./internal/webui/dist
RUN go build -trimpath -ldflags="-s -w" -o /out/splash ./cmd/splash

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/splash /splash
EXPOSE 8080
ENTRYPOINT ["/splash"]
