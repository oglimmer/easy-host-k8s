#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-docker}"
TAG="${2:-local}"

echo "==> Building with mode: $MODE, tag: $TAG"

if [ "$MODE" = "native" ]; then
  echo "--- Backend ---"
  cd backend
  ./mvnw clean package -DskipTests
  cd ..

elif [ "$MODE" = "docker" ]; then
  echo "--- Backend image ---"
  docker build -t "easy-host:$TAG" ./backend

else
  echo "Usage: $0 [native|docker] [tag]"
  exit 1
fi

echo "==> Build complete."
