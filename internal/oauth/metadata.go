package oauth

// Endpoint paths, relative to the issuer.
const (
	AuthorizePath             = "/oauth/authorize"
	TokenPath                 = "/oauth/token"
	RevocationPath            = "/oauth/revoke"
	ServerMetadataPath        = "/.well-known/oauth-authorization-server"
	ResourceMetadataPath      = "/.well-known/oauth-protected-resource"
	ResourceMetadataInsertion = ResourceMetadataPath + "/mcp"
)

// ServerMetadata is RFC 8414 authorization server metadata.
type ServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	RevocationEndpointAuthMethods     []string `json:"revocation_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	IssParameterSupported             bool     `json:"authorization_response_iss_parameter_supported"`
}

// ResourceMetadata is RFC 9728 protected resource metadata for /mcp.
type ResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// ServerMetadata describes this authorization server.
func (s *Service) ServerMetadata() ServerMetadata {
	return ServerMetadata{
		Issuer:                            s.issuer,
		AuthorizationEndpoint:             s.issuer + AuthorizePath,
		TokenEndpoint:                     s.issuer + TokenPath,
		ScopesSupported:                   Scopes,
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		RevocationEndpoint:                s.issuer + RevocationPath,
		RevocationEndpointAuthMethods:     []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ClientIDMetadataDocumentSupported: true,
		IssParameterSupported:             true,
	}
}

// ResourceMetadata describes /mcp as a protected resource.
func (s *Service) ResourceMetadata() ResourceMetadata {
	return ResourceMetadata{
		Resource:               s.resource,
		AuthorizationServers:   []string{s.issuer},
		ScopesSupported:        Scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Ticketopia",
	}
}

// ResourceMetadataURL is advertised in WWW-Authenticate challenges.
func (s *Service) ResourceMetadataURL() string { return s.issuer + ResourceMetadataInsertion }
