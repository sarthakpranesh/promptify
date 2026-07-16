#!/usr/bin/env bash

# Exit on error, undefined variables, and pipeline errors.
set -euo pipefail

# Docker Hub image coordinates.
DOCKERHUB_USERNAME="sarthakpranesh"
IMAGE_NAME="promptify"
TAG="0.0.2"

# Full image name to build and push (version tag + latest).
FULL_IMAGE_NAME="${DOCKERHUB_USERNAME}/${IMAGE_NAME}:${TAG}"
LATEST_IMAGE_NAME="${DOCKERHUB_USERNAME}/${IMAGE_NAME}:latest"

# Always multi-arch: linux/amd64 + linux/arm64 via a docker-container buildx builder.
# The default "docker" driver cannot build multiple platforms in one build (classic image store).
# See: https://docs.docker.com/go/build-multi-platform/
if ! docker buildx inspect multiarch >/dev/null 2>&1; then
	echo "Creating buildx builder 'multiarch' (driver: docker-container)..."
	docker buildx create --name multiarch --driver docker-container --use
else
	docker buildx use multiarch
fi

# Pull/build the buildkit image and ensure the builder is ready (first run can be slow).
docker buildx inspect --bootstrap

# Multi-arch images are pushed as a manifest list; use buildx --push (not docker push per tag).
# Cross-compiling non-native arches needs QEMU/binfmt for RUN steps that execute foreign binaries;
# pure Go (CGO_ENABLED=0) often only needs binfmt when emulating full stages. Docker Desktop
# usually includes this; on Linux you may need:
#   docker run --privileged --rm tonistiigi/binfmt --install all
echo "Building multi-platform (linux/amd64,linux/arm64): ${FULL_IMAGE_NAME}, ${LATEST_IMAGE_NAME}"
docker buildx build \
	--platform linux/amd64,linux/arm64 \
	-t "${FULL_IMAGE_NAME}" \
	-t "${LATEST_IMAGE_NAME}" \
	--push \
	.

echo "Done. Pushed ${FULL_IMAGE_NAME} and ${LATEST_IMAGE_NAME} (linux/amd64,linux/arm64)"
