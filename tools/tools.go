//go:build tools
// +build tools

package tools

import (
	_ "github.com/deepmap/oapi-codegen/cmd/oapi-codegen"
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway"
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2"
	_ "github.com/grpc-ecosystem/protoc-gen-grpc-gateway-ts"
	_ "github.com/jandelgado/gcov2lcov"
	_ "github.com/maxbrunsfeld/counterfeiter/v6"
	_ "github.com/onsi/ginkgo/v2/ginkgo"
	_ "github.com/ory/go-acc"
	_ "github.com/ory/go-acc/cmd"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)

// This file imports packages that are used when running go generate, or used
// during the development process but not otherwise depended on by built code.
