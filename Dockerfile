FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./cmd/controller/panelassets
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /aura-power-controller ./cmd/controller/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /aura-power ./cmd/aura-power/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /aura-power-controller /aura-power-controller
COPY --from=backend /aura-power /aura-power
USER 65534:65534
ENTRYPOINT ["/aura-power-controller"]
