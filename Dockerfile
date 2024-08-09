## Build the manager binary

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

ARG TARGETOS
ARG TARGETARCH

WORKDIR /
COPY build/bin/manager-${TARGETOS}-${TARGETARCH} manager
USER 65532:65532

ENTRYPOINT ["/manager"]
