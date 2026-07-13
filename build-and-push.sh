#!/usr/bin/env bash

# Exit on error, undefined variables, and pipeline errors.
set -euo pipefail

# Docker Hub image coordinates.
DOCKERHUB_USERNAME="sarthakpranesh"
IMAGE_NAME="promptify"
TAG="0.0.6"

# Comma-separated platforms for buildx (override: PLATFORMS=linux/arm64 ./build-and-push.sh).
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

# Buildx builder name (override: BUILDX_BUILDER=mybuilder ./build-and-push.sh).
BUILDX_BUILDER="${BUILDX_BUILDER:-multiarch}"

# Full image name to build and push (version tag + latest).
FULL_IMAGE_NAME="${DOCKERHUB_USERNAME}/${IMAGE_NAME}:${TAG}"
LATEST_IMAGE_NAME="${DOCKERHUB_USERNAME}/${IMAGE_NAME}:latest"

# The default "docker" driver cannot build multiple platforms in one build (classic image store).
# A "docker-container" builder runs BuildKit in a container and supports multi-arch manifest lists.
# See: https://docs.docker.com/go/build-multi-platform/
if ! docker buildx inspect "${BUILDX_BUILDER}" >/dev/null 2>&1; then
	echo "Creating buildx builder '${BUILDX_BUILDER}' (driver: docker-container)..."
	docker buildx create --name "${BUILDX_BUILDER}" --driver docker-container --use
else
	docker buildx use "${BUILDX_BUILDER}"
fi
# Pull/build the buildkit image and ensure the builder is ready (first run can be slow).
docker buildx inspect --bootstrap

# Multi-arch images are pushed as a manifest list; use buildx --push (not docker push per tag).
# Cross-compiling non-native arches needs QEMU/binfmt for RUN steps that execute foreign binaries;
# pure Go (CGO_ENABLED=0) often only needs binfmt when emulating full stages. Docker Desktop
# usually includes this; on Linux you may need:
#   docker run --privileged --rm tonistiigi/binfmt --install all
echo "Building multi-platform (${PLATFORMS}): ${FULL_IMAGE_NAME}, ${LATEST_IMAGE_NAME}"
docker buildx build \
	--platform "${PLATFORMS}" \
	-t "${FULL_IMAGE_NAME}" \
	-t "${LATEST_IMAGE_NAME}" \
	--push \
	.

echo "Done. Pushed ${FULL_IMAGE_NAME} and ${LATEST_IMAGE_NAME} (${PLATFORMS})"
