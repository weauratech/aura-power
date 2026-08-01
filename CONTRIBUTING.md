# Contributing to Aura Power

Thank you for your interest in contributing to Aura Power. This document provides guidelines and information for contributors.

## Getting Started

### Prerequisites

- Go 1.25+
- Node.js 20+
- Docker
- kubectl with access to a Kubernetes cluster
- Helm 3

### Development Setup

```bash
# Clone the repository
git clone https://github.com/weauratech/aura-power.git
cd aura-power

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web && npm ci && cd ..

# Run tests
make test-all

# Build binaries
make build-controller
make build-cli

# Build frontend
make build-web

# Run linter
golangci-lint run
```

### Running Locally

```bash
# Start the controller (requires kubeconfig)
go run ./cmd/controller/

# Start the server (requires kubeconfig + env vars)
JWT_SECRET=dev-secret AUTH_DB_PATH=./dev.db go run ./cmd/server/

# Start the frontend dev server (proxies API to localhost:8080)
cd web && npm run dev
```

## Architecture

Aura Power follows hexagonal architecture:

```
cmd/           - Entry points (server, controller, CLI)
internal/
  core/domain/ - Pure business logic (no external deps)
  ports/       - Interface contracts
  adapters/
    driven/    - Infrastructure (K8s, SQLite, Prometheus)
    driving/   - Entry adapters (HTTP API, Reconcilers)
api/v1alpha1/  - CRD type definitions
web/           - React panel (Vite + Chakra UI)
charts/        - Helm chart
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Ensure tests pass (`make test-all`)
5. Ensure linter passes (`golangci-lint run`)
6. Commit with conventional commit messages
7. Push and open a Pull Request

### Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — New feature
- `fix:` — Bug fix
- `docs:` — Documentation only
- `refactor:` — Code change that neither fixes a bug nor adds a feature
- `test:` — Adding or updating tests
- `chore:` — Maintenance tasks

### Code Style

- Go: Follow `gofmt` and `goimports` standards
- Frontend: Follow ESLint configuration
- Core domain (`internal/core/domain/`) must have zero external dependencies

## Reporting Issues

Please use GitHub Issues with the provided templates. Include:

- Aura Power version
- Kubernetes version
- Steps to reproduce
- Expected vs actual behavior

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
