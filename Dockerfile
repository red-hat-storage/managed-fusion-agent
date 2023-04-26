# Build the manager binary
FROM golang:1.19 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY utils/ utils/
COPY templates/ templates/
COPY cmd/awsDataGather/ awsDataGather/
COPY datafoundation/ datafoundation/
# TODO: Remove these when we have proper solution for CRD problem
# https://github.com/red-hat-storage/managed-fusion-agent/issues/28
COPY cmd/crdCreator crdCreator/
COPY shim/crds/noobaas.noobaa.io.yaml shim/crds/
COPY shim/crds/objectbucketclaims.objectbucket.io.yaml shim/crds/
COPY shim/crds/objectbuckets.objectbucket.io.yaml shim/crds

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

# Build aws data gathering binary
# Because the executable is the same name as the directory the source code is in,
# Go will build it as awsDataGather/main; the name will be changed during the copy operation.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o awsDataGather awsDataGather/main.go

# Build crdCreator binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o crdCreator crdCreator/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/awsDataGather/main awsDataGather
COPY --from=builder /workspace/crdCreator/main crdCreator
COPY --from=builder /workspace/templates/customernotification.html /templates/
COPY --from=builder /workspace/shim/crds/ /shim/crds
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
