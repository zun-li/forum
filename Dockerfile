FROM node:24-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/src ./src
COPY web/public ./public
COPY web/vite.config.js .
RUN npm run build

FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY web/*.go ./web/
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /forum ./cmd/forum

FROM alpine:3.23
WORKDIR /app
COPY --from=build /forum ./forum
COPY config/config.yaml ./config/config.yaml
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["./forum"]
