// Package api carries the OpenAPI description of the JSON API and serves it, so a
// client can read the contract from the server it is talking to rather than from a
// file somebody mailed them.
package api

import _ "embed"

// Spec is api/openapi.yaml, embedded in the binary.
//
//go:embed openapi.yaml
var Spec []byte

// SpecContentType is what the file is served as.
const SpecContentType = "application/yaml; charset=utf-8"

// Docs is the page a browser opens to read Spec: it renders the document the server
// is serving, so it is never out of date with the binary.
//
//go:embed docs.html
var Docs []byte
