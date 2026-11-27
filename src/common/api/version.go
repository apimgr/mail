package api

// Version is the API version used by every native route and client request.
const Version = "v1"

// BasePath is the canonical base path for native API routes.
const BasePath = "/api/" + Version

// APIBasePath returns the canonical native API route prefix.
func APIBasePath() string {
	return BasePath
}
