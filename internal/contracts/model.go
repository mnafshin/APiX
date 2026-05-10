package contracts

const CurrentSchemaVersion = "apix.contract/v1"

type Contract struct {
	SchemaVersion string     `json:"schema_version" yaml:"schema_version"`
	Info          Info       `json:"info" yaml:"info"`
	Servers       []Server   `json:"servers,omitempty" yaml:"servers,omitempty"`
	Endpoints     []Endpoint `json:"endpoints" yaml:"endpoints"`
}

type Info struct {
	Title       string `json:"title" yaml:"title"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Endpoint struct {
	Path       string               `json:"path" yaml:"path"`
	Operations map[string]Operation `json:"operations" yaml:"operations"`
}

type Operation struct {
	Summary     string            `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Parameters  []Parameter       `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *MediaTypeSchema  `json:"request_body,omitempty" yaml:"request_body,omitempty"`
	Responses   map[string]Result `json:"responses" yaml:"responses"`
	Auth        []AuthRequirement `json:"auth,omitempty" yaml:"auth,omitempty"`
	Examples    []Example         `json:"examples,omitempty" yaml:"examples,omitempty"`
}

type Parameter struct {
	Name        string   `json:"name" yaml:"name"`
	In          string   `json:"in" yaml:"in"` // path|query|header|cookie
	Required    bool     `json:"required,omitempty" yaml:"required,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      JSONType `json:"schema" yaml:"schema"`
}

type MediaTypeSchema struct {
	ContentType string                 `json:"content_type,omitempty" yaml:"content_type,omitempty"`
	Schema      map[string]any         `json:"schema,omitempty" yaml:"schema,omitempty"`
	Examples    map[string]ExampleData `json:"examples,omitempty" yaml:"examples,omitempty"`
}

type Result struct {
	Description string           `json:"description" yaml:"description"`
	Body        *MediaTypeSchema `json:"body,omitempty" yaml:"body,omitempty"`
}

type AuthRequirement struct {
	Type   string   `json:"type" yaml:"type"` // none|apiKey|http|oauth2|mtls
	Scheme string   `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

type Example struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Request     any    `json:"request,omitempty" yaml:"request,omitempty"`
	Response    any    `json:"response,omitempty" yaml:"response,omitempty"`
}

type ExampleData struct {
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`
	Value   any    `json:"value,omitempty" yaml:"value,omitempty"`
}

type JSONType struct {
	Type   string `json:"type" yaml:"type"` // string|integer|number|boolean|object|array
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}

func NewTemplate(title, version string) *Contract {
	return &Contract{
		SchemaVersion: CurrentSchemaVersion,
		Info: Info{
			Title:       title,
			Version:     version,
			Description: "Draft contract. Fill endpoint details before implementation.",
		},
		Endpoints: []Endpoint{
			{
				Path: "/health",
				Operations: map[string]Operation{
					"GET": {
						Summary: "Health check",
						Responses: map[string]Result{
							"200": {Description: "OK"},
						},
					},
				},
			},
		},
	}
}
